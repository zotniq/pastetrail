/* Exercise the real macOS destination path with unposted input events. This
 * deliberately runs off the main thread without an AppKit main run loop. */
#include "../capture.m"
#include <assert.h>
#include <pthread.h>
#include <string.h>
#include <unistd.h>

static pid_t fixture_focus;
static unsigned focus_queries;

static pid_t query_fixture_focus(void) {
    focus_queries++;
    return fixture_focus;
}

static NSDictionary *fixture_window(int pid, int number, int layer, double alpha, int width) {
    return @{
        (id)kCGWindowOwnerPID : @(pid),
        (id)kCGWindowNumber : @(number),
        (id)kCGWindowLayer : @(layer),
        (id)kCGWindowAlpha : @(alpha),
        (id)kCGWindowBounds : @{@"X" : @(-120), @"Y" : @0, @"Width" : @(width), @"Height" : @800}
    };
}

static void check_window_metadata(void) {
    NSArray *windows = @[
        fixture_window(200, 1, 0, 1, 1000), fixture_window(100, 2, 0, 0, 1000),
        fixture_window(100, 3, 5, 1, 1000), fixture_window(100, 4, 0, 1, 10),
        fixture_window(100, 5, 0, 1, 1000), fixture_window(100, 6, 0, 1, 1000)
    ];
    pt_json j;
    pt_json_init(&j);
    pt_put(&j, "{\"fixture\":true");
    window_metadata(&j, windows, 100);
    pt_put(&j, "}");
    char *json = pt_finish(&j);
    assert(json && strstr(json, "\"window\":\"5\"") && strstr(json, "\"x\":-120") &&
           strstr(json, "\"width\":1000"));
    free(json);
    pt_json_init(&j);
    pt_put(&j, "{\"fixture\":true");
    window_metadata(&j, windows, 999);
    pt_put(&j, "}");
    json = pt_finish(&j);
    assert(json && !strstr(json, "\"window\""));
    free(json);
}

static void check_pointer_bindings(void) {
    NSDictionary *bindings = @{
        @"Button,2,1,," : @{@"Action" : @"kPasteFromClipboardPointerAction"},
        @"Button,2,1,o," : @{@"Action" : @"kPasteFromSelectionPointerAction"},
        @"Button,2,2,cs^," : @{@"Action" : @"kPasteFromSelectionPointerAction"},
        @"Button,2,2,," : @{@"Action" : @"kOpenTargetPointerAction"},
        @"Button,2,3,," : @"invalid"
    };
    CGEventRef event =
        CGEventCreateMouseEvent(NULL, kCGEventOtherMouseUp, CGPointZero, kCGMouseButtonCenter);
    assert(event);
    CGEventSetIntegerValueField(event, kCGMouseEventClickState, 1);
    assert(!iterm_pointer_source(nil, event));
    assert(strcmp(iterm_pointer_source(bindings, event), "system_clipboard") == 0);
    CGEventSetFlags(event, kCGEventFlagMaskAlternate);
    assert(strcmp(iterm_pointer_source(bindings, event), "iterm_selection") == 0);
    CGEventSetIntegerValueField(event, kCGMouseEventClickState, 2);
    CGEventSetFlags(event,
                    kCGEventFlagMaskCommand | kCGEventFlagMaskShift | kCGEventFlagMaskControl);
    assert(strcmp(iterm_pointer_source(bindings, event), "iterm_selection") == 0);
    CGEventSetFlags(event, 0);
    assert(!iterm_pointer_source(bindings, event));
    CGEventSetIntegerValueField(event, kCGMouseEventClickState, 3);
    assert(!iterm_pointer_source(bindings, event));
    CGEventSetIntegerValueField(event, kCGMouseEventButtonNumber, 1);
    assert(!iterm_pointer_source(bindings, event));
    CGEventSetIntegerValueField(event, kCGMouseEventButtonNumber, 2);
    CGEventSetIntegerValueField(event, kCGEventTargetUnixProcessID, 0);
    fixture_focus = 111;
    unsigned before = focus_queries;
    assert(event_target(event, query_fixture_focus).pid == 0 && focus_queries == before);
    assert(!middle_click_source(event));
    CGEventSetIntegerValueField(event, kCGEventTargetUnixProcessID, 2147483001);
    assert(!middle_click_source(event));
    pt_reset();
    pt_limit = 128;
    capture(event, "middle_click", "iterm_selection"); // Never reads the system clipboard.
    char *json = pt_dequeue();
    assert(json && strstr(json, "iterm_selection") && strstr(json, "inside the application"));
    assert(!strstr(json, "\"formats\"") && !strstr(json, "\"files\""));
    free(json);
    pt_reset();
    CGEventSetType(event, kCGEventOtherMouseDown);
    assert(!iterm_pointer_source(bindings, event));
    CFRelease(event);
}

static NSDictionary *context_snapshot(CGEventRef event) {
    pt_json j;
    pt_json_init(&j);
    pt_put(&j, "{\"fixture\":true");
    destination_context(&j, event, query_fixture_focus, false);
    pt_put(&j, "}");
    char *json = pt_finish(&j);
    assert(json);
    NSData *data = [NSData dataWithBytes:json length:strlen(json)];
    free(json);
    NSError *error = nil;
    NSDictionary *result = [NSJSONSerialization JSONObjectWithData:data options:0 error:&error];
    assert(result && !error);
    return result;
}

static void *check_destinations(void *unused) {
    (void)unused;
    @autoreleasepool {
        assert(![NSThread isMainThread]);
        CGEventRef event = CGEventCreateKeyboardEvent(NULL, 9, true);
        assert(event);
        CGEventSetFlags(event, kCGEventFlagMaskCommand);
        CGEventSetIntegerValueField(event, kCGEventSourceUnixProcessID, 999);
        fixture_focus = 111; /* A stale/different foreground app must not win. */
        const pid_t targets[] = {2147483001, 2147483002, 2147483003, 2147483001, getpid()};
        for (unsigned i = 0; i < sizeof(targets) / sizeof(targets[0]); i++) {
            CGEventSetIntegerValueField(event, kCGEventTargetUnixProcessID, targets[i]);
            paste_target target = event_target(event, query_fixture_focus);
            assert(target.pid == targets[i]);
            assert(strcmp(target.method, "event_target_pid") == 0);
            NSDictionary *snapshot = context_snapshot(event);
            assert([snapshot[@"pid"] intValue] == targets[i]);
            assert([snapshot[@"target_method"] isEqualToString:@"event_target_pid"]);
        }
        assert(focus_queries == 0);
        const int64_t invalid[] = {0, -1, (int64_t)INT_MAX + 1, INT64_MAX};
        for (unsigned i = 0; i < sizeof(invalid) / sizeof(invalid[0]); i++) {
            CGEventSetIntegerValueField(event, kCGEventTargetUnixProcessID, invalid[i]);
            paste_target target = event_target(event, query_fixture_focus);
            assert(target.pid == fixture_focus);
            assert(strcmp(target.method, "accessibility_focus") == 0);
        }
        assert(focus_queries == 4);
        assert(event_target(NULL, query_fixture_focus).pid == fixture_focus);
        CGEventSetIntegerValueField(event, kCGEventTargetUnixProcessID, 0);
        NSDictionary *fallback = context_snapshot(event);
        assert([fallback[@"pid"] intValue] == fixture_focus);
        assert([fallback[@"target_method"] isEqualToString:@"accessibility_focus"]);
        fixture_focus = 0;
        NSDictionary *missing = context_snapshot(event);
        assert(!missing[@"pid"] && !missing[@"app"] && !missing[@"title"] && !missing[@"document"]);
        assert([missing[@"target_method"] isEqualToString:@"unavailable"]);
        fixture_focus = -1;
        assert(event_target(event, query_fixture_focus).pid == 0);
        CFRelease(event);
        check_pointer_bindings();
        check_window_metadata();
    }
    return NULL;
}

int main(void) {
    pthread_t thread;
    assert(pthread_create(&thread, NULL, check_destinations, NULL) == 0);
    assert(pthread_join(thread, NULL) == 0);
    puts("macOS event destinations, focus fallback, and missing-target regression checks passed");
    return 0;
}
