//go:build darwin && cgo

#include "native.h"
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#include <stdio.h>

static CFMachPortRef tap;
static CFRunLoopSourceRef tap_source;
static CFRunLoopRef run_loop;

const char *pt_probe(void) {
    if (!CGPreflightListenEventAccess())
        return "permission_required|grant Input Monitoring to the terminal or installed executable "
               "in System Settings; no permission prompt is requested automatically";
    return "supported|Cmd+V variants on the standard V key location; custom layouts, menu pastes, "
           "app-internal pastes, and secure-input sessions may be unobservable";
}

static NSString *ax_string(AXUIElementRef element, CFStringRef attribute) {
    CFTypeRef value = NULL;
    if (AXUIElementCopyAttributeValue(element, attribute, &value) != kAXErrorSuccess || !value)
        return nil;
    NSString *result = nil;
    if (CFGetTypeID(value) == CFStringGetTypeID())
        result = [(__bridge NSString *)value copy];
    else if (CFGetTypeID(value) == CFURLGetTypeID())
        result = [[(__bridge NSURL *)value absoluteString] copy];
    CFRelease(value);
    return [result autorelease];
}

static void capture(const char *action) {
    @autoreleasepool {
        pt_json j;
        pt_event_begin(&j, "keyboard", action, "system_clipboard");
        NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
        if (app) {
            pt_put(&j, ",\"pid\":");
            pt_number(&j, [app processIdentifier]);
            pt_property(&j, "app", [[app localizedName] UTF8String]);
            pt_property(&j, "executable", [[[app executableURL] path] UTF8String]);
            if (AXIsProcessTrusted()) {
                AXUIElementRef application = AXUIElementCreateApplication([app processIdentifier]);
                AXUIElementSetMessagingTimeout(application, 0.03);
                CFTypeRef window = NULL;
                if (AXUIElementCopyAttributeValue(application, kAXFocusedWindowAttribute,
                                                  &window) == kAXErrorSuccess &&
                    window) {
                    NSString *title = ax_string((AXUIElementRef)window, kAXTitleAttribute);
                    NSString *document = ax_string((AXUIElementRef)window, kAXDocumentAttribute);
                    if (title)
                        pt_property(&j, "title", [title UTF8String]);
                    if (document)
                        pt_property(&j, "document", [document UTF8String]);
                    CFRelease(window);
                }
                CFRelease(application);
            }
        }
        NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
        NSInteger generation = [pasteboard changeCount];
        pt_put(&j, ",\"formats\":[");
        int first = 1;
        NSArray *types = @[ NSPasteboardTypeString, NSPasteboardTypeHTML, NSPasteboardTypeRTF ];
        const char *media[] = {"text/plain", "text/html", "text/rtf"};
        for (NSUInteger i = 0; i < [types count]; i++) {
            NSData *data = [pasteboard dataForType:[types objectAtIndex:i]];
            if (data)
                pt_format(&j, media[i], [data bytes], [data length], &first);
        }
        pt_put(&j, "],\"files\":[");
        first = 1;
        NSUInteger files = 0;
        for (NSPasteboardItem *item in [pasteboard pasteboardItems]) {
            NSString *uri = [item stringForType:NSPasteboardTypeFileURL];
            if (uri) {
                if (files++ == 1024)
                    break;
                if (!first)
                    pt_put(&j, ",");
                first = 0;
                pt_string(&j, [uri UTF8String]);
            }
        }
        pt_put(&j, "]");
        if (files > 1024)
            pt_property(&j, "issue", "file reference count exceeds 1024; list truncated");
        else if (generation != [pasteboard changeCount])
            pt_property(
                &j, "issue",
                "clipboard changed during capture; representations may not match the attempt");
        pt_put(&j, ",\"captured_ns\":");
        pt_number(&j, pt_now());
        pt_put(&j, "}");
        pt_enqueue(pt_finish(&j));
    }
}

static CGEventRef callback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *user) {
    (void)proxy;
    (void)user;
    if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
        snprintf(
            pt_failure, sizeof(pt_failure),
            "input event tap was disabled; monitoring stopped and attempts may have been missed");
        return event;
    }
    if (type != kCGEventKeyDown)
        return event;
    CGEventFlags flags = CGEventGetFlags(event);
    if (CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode) == 9 &&
        (flags & kCGEventFlagMaskCommand) && !(flags & kCGEventFlagMaskControl)) {
        const char *action =
            (flags & kCGEventFlagMaskShift)
                ? ((flags & kCGEventFlagMaskAlternate) ? "cmd+option+shift+v" : "cmd+shift+v")
                : ((flags & kCGEventFlagMaskAlternate) ? "cmd+option+v" : "cmd+v");
        capture(action);
    }
    return event;
}

int pt_open(int limit) {
    pt_reset();
    pt_limit = limit;
    if (!CGPreflightListenEventAccess()) {
        snprintf(pt_failure, sizeof(pt_failure),
                 "Input Monitoring permission is required; run pastetrail doctor");
        return 0;
    }
    tap = CGEventTapCreate(kCGSessionEventTap, kCGTailAppendEventTap, kCGEventTapOptionListenOnly,
                           CGEventMaskBit(kCGEventKeyDown), callback, NULL);
    if (!tap) {
        snprintf(pt_failure, sizeof(pt_failure), "could not create a passive input event tap");
        return 0;
    }
    tap_source = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0);
    if (!tap_source) {
        CFRelease(tap);
        tap = NULL;
        snprintf(pt_failure, sizeof(pt_failure), "could not create an input run-loop source");
        return 0;
    }
    run_loop = CFRunLoopGetCurrent();
    CFRetain(run_loop);
    CFRunLoopAddSource(run_loop, tap_source, kCFRunLoopCommonModes);
    CGEventTapEnable(tap, true);
    return 1;
}

char *pt_next(int timeout_ms) {
    char *pending = pt_dequeue();
    if (pending)
        return pending;
    CFRunLoopRunInMode(kCFRunLoopDefaultMode, (double)timeout_ms / 1000.0, true);
    return pt_dequeue();
}

void pt_close(void) {
    if (tap)
        CGEventTapEnable(tap, false);
    if (tap_source) {
        CFRunLoopRemoveSource(run_loop, tap_source, kCFRunLoopCommonModes);
        CFRelease(tap_source);
        tap_source = NULL;
    }
    if (tap) {
        CFMachPortInvalidate(tap);
        CFRelease(tap);
        tap = NULL;
    }
    if (run_loop) {
        CFRelease(run_loop);
        run_loop = NULL;
    }
    pt_reset();
}
