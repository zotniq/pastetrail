//go:build darwin && cgo

#include "../native/native.h"
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#include <limits.h>
#include <libproc.h>
#include <stdio.h>
#include <string.h>
#include "focus.h"

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

// A browser bundle whose active tab URL is readable from the accessibility tree.
// Go's browser.kindForBundle validates the attribution; this only decides whether
// the native capture pays for an AXWebArea traversal.
static bool browser_bundle(NSString *bundle) {
    if (![bundle length])
        return false;
    NSString *lower = [bundle lowercaseString];
    NSArray *needles = @[
        @"com.apple.safari", @"com.google.chrome", @"com.google.chrome.", @"com.brave.browser",
        @"com.microsoft.edgemac", @"com.operasoftware.", @"com.vivaldi.vivaldi",
        @"company.thebrowser.browser", @"com.mighty.app", @"com.ghostbrowser.gb1",
        @"com.bookry.wavebox", @"com.pushplaylabs.sidekick", @"org.chromium.chromium"
    ];
    for (NSString *needle in needles)
        if ([lower hasPrefix:needle] || [lower isEqualToString:needle])
            return true;
    return false;
}

// Find the active tab URL by locating the focused window's web area. This uses
// only the Accessibility permission already granted to inspect focused controls,
// so no per-browser Automation (Apple Events) authorization is needed.
static NSString *ax_browser_url(AXUIElementRef window) {
    NSMutableArray *queue = [NSMutableArray arrayWithObject:(__bridge id)window];
    double deadline = focus_time() + 0.06;
    NSUInteger budget = 600;
    for (NSUInteger i = 0; i < [queue count] && budget-- > 0 && focus_time() < deadline; i++) {
        AXUIElementRef element = (__bridge AXUIElementRef)[queue objectAtIndex:i];
        AXUIElementSetMessagingTimeout(element, 0.03);
        NSString *role = ax_string(element, kAXRoleAttribute);
        if ([role isEqualToString:@"AXWebArea"]) {
            NSString *url = ax_string(element, kAXURLAttribute);
            if ([url hasPrefix:@"http://"] || [url hasPrefix:@"https://"])
                return url;
        }
        CFTypeRef children = NULL;
        if (AXUIElementCopyAttributeValue(element, kAXChildrenAttribute, &children) ==
                kAXErrorSuccess &&
            children) {
            if (CFGetTypeID(children) == CFArrayGetTypeID())
                [queue addObjectsFromArray:(__bridge NSArray *)children];
            CFRelease(children);
        }
    }
    return nil;
}

static pid_t focused_pid(void) {
    if (!AXIsProcessTrusted())
        return 0;
    AXUIElementRef system = AXUIElementCreateSystemWide();
    AXUIElementSetMessagingTimeout(system, 0.03);
    CFTypeRef application = NULL;
    pid_t pid = 0;
    if (AXUIElementCopyAttributeValue(system, kAXFocusedApplicationAttribute, &application) ==
            kAXErrorSuccess &&
        application && CFGetTypeID(application) == AXUIElementGetTypeID()) {
        if (AXUIElementGetPid((AXUIElementRef)application, &pid) != kAXErrorSuccess)
            pid = 0;
    }
    if (application)
        CFRelease(application);
    CFRelease(system);
    return pid > 0 ? pid : 0;
}

typedef struct {
    pid_t pid;
    const char *method;
} paste_target;

static paste_target event_target(CGEventRef event, pid_t (*query_focus)(void)) {
    int64_t pid = event ? CGEventGetIntegerValueField(event, kCGEventTargetUnixProcessID) : 0;
    if (pid > 0 && pid <= INT_MAX)
        return (paste_target){(pid_t)pid, "event_target_pid"};
    // A click may target a background window; keyboard focus is not its recipient.
    if (event && CGEventGetType(event) == kCGEventOtherMouseUp)
        return (paste_target){0, "unavailable"};
    pid_t focused = query_focus();
    if (focused > 0)
        return (paste_target){focused, "accessibility_focus"};
    return (paste_target){0, "unavailable"};
}

static pid_t destination_context(pt_json *j, CGEventRef event, pid_t (*query_focus)(void),
                                 bool accessibility) {
    /* NSWorkspace.frontmostApplication can remain cached until the MAIN run
     * loop advances. A Go library must not require its host to run AppKit's
     * main loop. Resolve each event's recipient before looking up app metadata. */
    paste_target target = event_target(event, query_focus);
    pt_property(j, "target_method", target.method);
    if (!target.pid) {
        focus_unavailable(j, "input event target application is unavailable");
        return 0;
    }
    pt_put(j, ",\"pid\":");
    pt_number(j, target.pid);
    NSRunningApplication *app =
        [NSRunningApplication runningApplicationWithProcessIdentifier:target.pid];
    if (app && [app processIdentifier] == target.pid) {
        pt_property(j, "app", [[app localizedName] UTF8String]);
        pt_property(j, "executable", [[[app executableURL] path] UTF8String]);
        pt_property(j, "bundle_id", [[app bundleIdentifier] UTF8String]);
        pt_property(j, "bundle_path", [[[app bundleURL] path] UTF8String]);
    }
    if (!accessibility || (event && CGEventGetType(event) == kCGEventOtherMouseUp)) {
        focus_unavailable(j, !accessibility ? "Accessibility permission is required to inspect the focused control"
                                           : "keyboard focus does not identify the clicked control");
        return target.pid;
    }
    AXUIElementRef application = AXUIElementCreateApplication(target.pid);
    focus_context(j, application, AXUIElementCopyAttributeValue,
                  AXUIElementCopyMultipleAttributeValues);
    AXUIElementSetMessagingTimeout(application, 0.03);
    // Chromium builds its accessibility tree only once an assistive client asks.
    // Requesting it here lets the first browser paste resolve a URL without a
    // per-application Automation grant.
    bool browser = browser_bundle([[NSRunningApplication
        runningApplicationWithProcessIdentifier:target.pid] bundleIdentifier]);
    if (browser)
        AXUIElementSetAttributeValue(application, CFSTR("AXManualAccessibility"), kCFBooleanTrue);
    CFTypeRef window = NULL;
    if (AXUIElementCopyAttributeValue(application, kAXFocusedWindowAttribute, &window) ==
            kAXErrorSuccess &&
        window && CFGetTypeID(window) == AXUIElementGetTypeID()) {
        AXUIElementSetMessagingTimeout((AXUIElementRef)window, 0.03);
        NSString *title = ax_string((AXUIElementRef)window, kAXTitleAttribute);
        NSString *document = ax_string((AXUIElementRef)window, kAXDocumentAttribute);
        if (title)
            pt_property(j, "title", [title UTF8String]);
        if (document)
            pt_property(j, "document", [document UTF8String]);
        if (browser) {
            NSString *url = ax_browser_url((AXUIElementRef)window);
            if (url)
                pt_property(j, "browser_url", [url UTF8String]);
        }
    }
    if (window)
        CFRelease(window);
    CFRelease(application);
    return target.pid;
}

static void window_metadata(pt_json *j, NSArray *windows, pid_t pid) {
    for (NSDictionary *window in windows) {
        if ([window[(id)kCGWindowOwnerPID] intValue] != pid ||
            [window[(id)kCGWindowLayer] intValue] != 0 ||
            [window[(id)kCGWindowAlpha] doubleValue] == 0)
            continue;
        CGRect bounds;
        id rectangle = window[(id)kCGWindowBounds];
        if (![rectangle isKindOfClass:[NSDictionary class]] ||
            !CGRectMakeWithDictionaryRepresentation((__bridge CFDictionaryRef)rectangle, &bounds) ||
            bounds.size.width < 50 || bounds.size.height < 50)
            continue;
        pt_property(j, "window", [[window[(id)kCGWindowNumber] stringValue] UTF8String]);
        pt_property(j, "window_method", "frontmost_window_for_event_pid");
        pt_bounds(j, "bounds", bounds.origin.x, bounds.origin.y, bounds.size.width,
                  bounds.size.height);
        break;
    }
}

static void window_context(pt_json *j, pid_t pid, bool keyboard) {
    if (pid <= 0)
        return;
    CFArrayRef windows = keyboard
                             ? CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly |
                                                              kCGWindowListExcludeDesktopElements,
                                                          kCGNullWindowID)
                             : NULL;
    if (windows) {
        window_metadata(j, (__bridge NSArray *)windows, pid);
        CFRelease(windows);
    }
    struct proc_taskinfo info;
    if (proc_pidinfo(pid, PROC_PIDTASKINFO, 0, &info, sizeof(info)) == sizeof(info)) {
        pt_put(j, ",\"memory_usage\":");
        pt_number(j, info.pti_resident_size);
    }
}

static const char *iterm_pointer_source(NSDictionary *bindings, CGEventRef event) {
    if (![bindings isKindOfClass:[NSDictionary class]] ||
        CGEventGetType(event) != kCGEventOtherMouseUp ||
        CGEventGetIntegerValueField(event, kCGMouseEventButtonNumber) != 2)
        return NULL;
    CGEventFlags flags = CGEventGetFlags(event);
    NSString *mods =
        [NSString stringWithFormat:@"%@%@%@%@", flags & kCGEventFlagMaskCommand ? @"c" : @"",
                                   flags & kCGEventFlagMaskAlternate ? @"o" : @"",
                                   flags & kCGEventFlagMaskShift ? @"s" : @"",
                                   flags & kCGEventFlagMaskControl ? @"^" : @""];
    NSString *key = [NSString
        stringWithFormat:@"Button,2,%lld,%@,",
                         (long long)CGEventGetIntegerValueField(event, kCGMouseEventClickState),
                         mods];
    id binding = bindings[key];
    if (![binding isKindOfClass:[NSDictionary class]])
        return NULL;
    id action = binding[@"Action"];
    if ([action isEqual:@"kPasteFromClipboardPointerAction"])
        return "system_clipboard";
    if ([action isEqual:@"kPasteFromSelectionPointerAction"])
        return "iterm_selection";
    return NULL;
}

static const char *middle_click_source(CGEventRef event) {
    int64_t pid = CGEventGetIntegerValueField(event, kCGEventTargetUnixProcessID);
    if (pid <= 0 || pid > INT_MAX)
        return NULL;
    NSRunningApplication *app =
        [NSRunningApplication runningApplicationWithProcessIdentifier:(pid_t)pid];
    if (![[app bundleIdentifier] isEqualToString:@"com.googlecode.iterm2"])
        return NULL;
    CFPropertyListRef custom = CFPreferencesCopyAppValue(CFSTR("LoadPrefsFromCustomFolder"),
                                                         CFSTR("com.googlecode.iterm2"));
    bool has_custom =
        custom && CFGetTypeID(custom) == CFBooleanGetTypeID() && CFBooleanGetValue(custom);
    if (custom)
        CFRelease(custom);
    if (has_custom)
        return NULL;
    CFPropertyListRef bindings =
        CFPreferencesCopyAppValue(CFSTR("PointerActions"), CFSTR("com.googlecode.iterm2"));
    const char *source = iterm_pointer_source((__bridge NSDictionary *)bindings, event);
    if (bindings)
        CFRelease(bindings);
    return source;
}

static void capture(CGEventRef event, const char *action, const char *source) {
    @autoreleasepool {
        pt_json j;
        bool mouse = CGEventGetType(event) == kCGEventOtherMouseUp;
        pt_event_begin(&j, mouse ? "mouse" : "keyboard", action, source);
        if (mouse) {
            pt_put(&j, ",\"button\":2,\"clicks\":");
            pt_number(&j, CGEventGetIntegerValueField(event, kCGMouseEventClickState));
            pt_put(&j, ",\"modifiers\":[");
            const CGEventFlags masks[] = {kCGEventFlagMaskCommand, kCGEventFlagMaskAlternate,
                                          kCGEventFlagMaskShift, kCGEventFlagMaskControl};
            const char *names[] = {"cmd", "option", "shift", "ctrl"};
            bool first = true;
            for (unsigned i = 0; i < 4; i++) {
                if (!(CGEventGetFlags(event) & masks[i]))
                    continue;
                if (!first)
                    pt_put(&j, ",");
                pt_string(&j, names[i]);
                first = false;
            }
            pt_put(&j, "]");
            pt_property(&j, "trigger_reason",
                        "iTerm2 saved pointer binding; mouse reporting or runtime settings may "
                        "override it");
        }
        pid_t pid = destination_context(&j, event, focused_pid, AXIsProcessTrusted());
        window_context(&j, pid, !mouse);
        if (strcmp(source, "iterm_selection") == 0) {
            pt_property(&j, "issue",
                        "iTerm2 keeps its most recent selection inside the application; its "
                        "content is unavailable to this standalone observer");
        } else {
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
        }
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
    if (type != kCGEventKeyDown) {
        if (type == kCGEventOtherMouseUp) {
            @autoreleasepool {
                const char *source = middle_click_source(event);
                if (source)
                    capture(event, "middle_click", source);
            }
        }
        return event;
    }
    CGEventFlags flags = CGEventGetFlags(event);
    if (CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode) == 9 &&
        (flags & kCGEventFlagMaskCommand) && !(flags & kCGEventFlagMaskControl)) {
        const char *action =
            (flags & kCGEventFlagMaskShift)
                ? ((flags & kCGEventFlagMaskAlternate) ? "cmd+option+shift+v" : "cmd+shift+v")
                : ((flags & kCGEventFlagMaskAlternate) ? "cmd+option+v" : "cmd+v");
        capture(event, action, "system_clipboard");
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
    /* At the annotated tap, events have been routed to a target process. */
    tap = CGEventTapCreate(
        kCGAnnotatedSessionEventTap, kCGTailAppendEventTap, kCGEventTapOptionListenOnly,
        CGEventMaskBit(kCGEventKeyDown) | CGEventMaskBit(kCGEventOtherMouseUp), callback, NULL);
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
