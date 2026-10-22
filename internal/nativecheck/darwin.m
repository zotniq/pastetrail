/* Exercise the real macOS destination path with unposted input events. This
 * deliberately runs off the main thread without an AppKit main run loop. */
#include "../../native_darwin.m"
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
