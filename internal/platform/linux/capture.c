//go:build linux && cgo

#include "../native/native.h"
#include <errno.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>
#include <xcb/record.h>
#include <xcb/xcb.h>
#include <xcb/xcbext.h>

static xcb_connection_t *control, *events;
static xcb_record_context_t recording;
static unsigned record_sequence;
static xcb_window_t root, request_window;
static int selection_incomplete;

static int64_t monotonic_ms(void) {
    struct timespec now;
    clock_gettime(CLOCK_MONOTONIC, &now);
    return (int64_t)now.tv_sec * 1000 + now.tv_nsec / 1000000;
}

static int is_wayland(void) {
    const char *type = getenv("XDG_SESSION_TYPE");
    const char *socket = getenv("WAYLAND_DISPLAY");
    return (type && strcmp(type, "wayland") == 0) || (socket && *socket);
}

const char *pt_probe(void) {
    if (is_wayland())
        return "unavailable|native Wayland capture requires compositor integration; XWayland does "
               "not provide desktop-wide coverage";
    if (!getenv("DISPLAY"))
        return "unavailable|DISPLAY is not set; an interactive X11 session is required";
    return "supported|requires X11 RECORD; Ctrl+V, Ctrl+Shift+V, Shift+Insert and PRIMARY "
           "middle-click attempts; custom bindings and keyboard groups may be missed";
}

/* XCB reports disconnects as errors instead of terminating the host process.
 * Bound reply waits so a stalled display or selection owner can be cancelled. */
static void wait_input(xcb_connection_t *connection, int timeout_ms) {
    struct pollfd fd = {xcb_get_file_descriptor(connection), POLLIN, 0};
    if (poll(&fd, 1, timeout_ms) < 0 && errno != EINTR)
        snprintf(pt_failure, sizeof(pt_failure), "X11 connection polling failed");
}

static void *reply(xcb_connection_t *connection, unsigned sequence) {
    int64_t deadline = monotonic_ms() + 100;
    xcb_flush(connection);
    do {
        void *result = NULL;
        xcb_generic_error_t *error = NULL;
        if (xcb_poll_for_reply(connection, sequence, &result, &error)) {
            free(error);
            return result;
        }
        if (xcb_connection_has_error(connection))
            break;
        wait_input(connection, 1);
    } while (monotonic_ms() < deadline);
    xcb_discard_reply(connection, sequence);
    return NULL;
}

static xcb_atom_t atom(const char *name) {
    xcb_intern_atom_reply_t *r =
        reply(control, xcb_intern_atom(control, 0, (uint16_t)strlen(name), name).sequence);
    xcb_atom_t result = r ? r->atom : XCB_NONE;
    free(r);
    return result;
}

static xcb_get_property_reply_t *property(xcb_window_t window, xcb_atom_t name, uint32_t limit) {
    return reply(
        control,
        xcb_get_property(control, 0, window, name, XCB_GET_PROPERTY_TYPE_ANY, 0, limit).sequence);
}

static int string_property(pt_json *j, xcb_window_t window, const char *name, const char *key) {
    int found = 0;
    xcb_get_property_reply_t *r = property(window, atom(name), 8192);
    if (r && r->format == 8) {
        int n = xcb_get_property_value_length(r);
        char *value = calloc((size_t)n + 1, 1);
        if (value) {
            memcpy(value, xcb_get_property_value(r), (size_t)n);
            pt_property(j, key, value);
            found = 1;
            free(value);
        }
    }
    free(r);
    return found;
}

static void destination(pt_json *j) {
    xcb_window_t window = XCB_NONE;
    xcb_get_property_reply_t *r = property(root, atom("_NET_ACTIVE_WINDOW"), 1);
    if (r && r->format == 32 && xcb_get_property_value_length(r) >= 4)
        memcpy(&window, xcb_get_property_value(r), 4);
    free(r);
    if (!window) {
        xcb_get_input_focus_reply_t *focus = reply(control, xcb_get_input_focus(control).sequence);
        if (focus)
            window = focus->focus;
        free(focus);
    }
    if (!window || window == XCB_INPUT_FOCUS_POINTER_ROOT)
        return;
    char id[32];
    snprintf(id, sizeof(id), "%u", window);
    pt_property(j, "window", id);
    xcb_get_geometry_reply_t *geometry = reply(control, xcb_get_geometry(control, window).sequence);
    xcb_translate_coordinates_reply_t *position =
        reply(control, xcb_translate_coordinates(control, window, root, 0, 0).sequence);
    if (geometry && position) {
        int64_t x = position->dst_x, y = position->dst_y, width = geometry->width,
                height = geometry->height;
        r = property(window, atom("_NET_FRAME_EXTENTS"), 4);
        if (r && r->format == 32 && xcb_get_property_value_length(r) == 16) {
            uint32_t extents[4];
            memcpy(extents, xcb_get_property_value(r), sizeof(extents));
            x -= extents[0];
            y -= extents[2];
            width += (int64_t)extents[0] + extents[1];
            height += (int64_t)extents[2] + extents[3];
        }
        free(r);
        pt_bounds(j, "bounds", x, y, width, height);
    }
    free(geometry);
    free(position);
    if (!string_property(j, window, "_NET_WM_NAME", "title"))
        string_property(j, window, "WM_NAME", "title");
    r = property(window, XCB_ATOM_WM_CLASS, 8192);
    if (r && r->format == 8) {
        size_t n = (size_t)xcb_get_property_value_length(r);
        const char *value = xcb_get_property_value(r);
        size_t instance = strnlen(value, n);
        if (instance + 1 < n) {
            const char *class_name = value + instance + 1;
            size_t remaining = n - instance - 1;
            if (strnlen(class_name, remaining) < remaining)
                pt_property(j, "app", class_name);
        }
    }
    free(r);
    /* A remote X11 client's PID must never be interpreted as a local PID. */
    char host[256] = {0};
    gethostname(host, sizeof(host) - 1);
    int local = 0;
    r = property(window, atom("WM_CLIENT_MACHINE"), 256);
    if (r && r->format == 8) {
        size_t n = (size_t)xcb_get_property_value_length(r);
        local = n >= strlen(host) && strnlen(xcb_get_property_value(r), n) == strlen(host) &&
                memcmp(xcb_get_property_value(r), host, strlen(host)) == 0;
    }
    free(r);
    if (!local)
        return;
    r = property(window, atom("_NET_WM_PID"), 1);
    if (r && r->format == 32 && xcb_get_property_value_length(r) >= 4) {
        uint32_t pid;
        memcpy(&pid, xcb_get_property_value(r), 4);
        pt_put(j, ",\"pid\":");
        pt_number(j, pid);
        char proc[64], exe[4096];
        snprintf(proc, sizeof(proc), "/proc/%u/exe", pid);
        ssize_t n = readlink(proc, exe, sizeof(exe) - 1);
        if (n > 0) {
            exe[n] = 0;
            pt_property(j, "executable", exe);
        }
        snprintf(proc, sizeof(proc), "/proc/%u/statm", pid);
        FILE *statm = fopen(proc, "r");
        if (statm) {
            unsigned long long pages, resident;
            long page_size = sysconf(_SC_PAGESIZE);
            if (fscanf(statm, "%llu %llu", &pages, &resident) == 2 && page_size > 0 &&
                resident <= INT64_MAX / (uint64_t)page_size) {
                pt_put(j, ",\"memory_usage\":");
                pt_number(j, resident * (uint64_t)page_size);
            }
            fclose(statm);
        }
    }
    free(r);
}

static xcb_window_t selection_owner(xcb_atom_t selection) {
    xcb_get_selection_owner_reply_t *r =
        reply(control, xcb_get_selection_owner(control, selection).sequence);
    xcb_window_t owner = r ? r->owner : XCB_NONE;
    if (!r)
        selection_incomplete = 1;
    free(r);
    return owner;
}

static unsigned char *selection(xcb_atom_t clipboard, const char *target_name, size_t *length) {
    xcb_atom_t target = atom(target_name), prop = atom("_PASTETRAIL_SELECTION");
    xcb_generic_event_t *event;
    while ((event = xcb_poll_for_event(control)))
        free(event);
    xcb_delete_property(control, request_window, prop);
    xcb_convert_selection(control, request_window, clipboard, target, prop, XCB_CURRENT_TIME);
    xcb_flush(control);
    int64_t deadline = monotonic_ms() + 50;
    do {
        while ((event = xcb_poll_for_event(control))) {
            if ((event->response_type & 0x7f) != XCB_SELECTION_NOTIFY) {
                free(event);
                continue;
            }
            xcb_selection_notify_event_t notify = *(xcb_selection_notify_event_t *)event;
            free(event);
            if (notify.selection != clipboard || notify.target != target ||
                notify.requestor != request_window)
                continue;
            if (!notify.property)
                return NULL;
            xcb_get_property_reply_t *r =
                property(request_window, prop, ((uint32_t)pt_limit + 3) / 4);
            if (!r)
                break;
            if (r->format != 8) {
                free(r);
                selection_incomplete = 1; /* Includes unsupported INCR transfers. */
                return NULL;
            }
            *length = (size_t)xcb_get_property_value_length(r);
            unsigned char *data = calloc(*length + 1, 1);
            if (data)
                memcpy(data, xcb_get_property_value(r), *length);
            if (!data || r->bytes_after)
                selection_incomplete = 1;
            free(r);
            return data;
        }
        if (xcb_connection_has_error(control))
            break;
        wait_input(control, 1);
    } while (monotonic_ms() < deadline);
    selection_incomplete = 1;
    return NULL;
}

static void capture(const char *kind, const char *action, xcb_atom_t clipboard) {
    pt_json j;
    pt_event_begin(&j, kind, action,
                   clipboard == XCB_ATOM_PRIMARY ? "primary_selection" : "system_clipboard");
    /* Middle-click may target an unfocused window. Do not label the focused
     * application as its recipient without pointer-window attribution. */
    if (clipboard != XCB_ATOM_PRIMARY)
        destination(&j);
    selection_incomplete = 0;
    xcb_window_t owner = selection_owner(clipboard);
    pt_put(&j, ",\"formats\":[");
    int first = 1;
    size_t n = 0;
    if (owner) {
        const char *targets[] = {"UTF8_STRING", "text/html", "text/rtf"};
        const char *media[] = {"text/plain", "text/html", "text/rtf"};
        for (int i = 0; i < 3; i++) {
            unsigned char *data = selection(clipboard, targets[i], &n);
            if (!data && i == 0)
                data = selection(clipboard, "text/plain;charset=utf-8", &n);
            if (data) {
                pt_format(&j, media[i], data, n, &first);
                free(data);
            }
        }
    }
    pt_put(&j, "],\"files\":[");
    first = 1;
    if (owner) {
        unsigned char *data = selection(clipboard, "text/uri-list", &n);
        if (!data)
            data = selection(clipboard, "x-special/gnome-copied-files", &n);
        if (data) {
            size_t start = 0;
            unsigned files = 0;
            for (size_t i = 0; i <= n; i++) {
                if (i != n && data[i] != '\n')
                    continue;
                size_t end = i;
                while (end > start && data[end - 1] == '\r')
                    end--;
                if (end > start && data[start] != '#' &&
                    !(end - start == 4 && memcmp(data + start, "copy", 4) == 0) &&
                    !(end - start == 3 && memcmp(data + start, "cut", 3) == 0)) {
                    if (files++ == 1024) {
                        selection_incomplete = 1;
                        break;
                    }
                    unsigned char saved = data[end];
                    data[end] = 0;
                    if (!first)
                        pt_put(&j, ",");
                    first = 0;
                    pt_string(&j, (char *)data + start);
                    data[end] = saved;
                }
                start = i + 1;
            }
            free(data);
        }
    }
    pt_put(&j, "]");
    xcb_window_t after = selection_owner(clipboard);
    if (selection_incomplete)
        pt_property(&j, "issue",
                    "clipboard transfer timed out, was incremental, or exceeded capture limits; "
                    "content may be incomplete");
    else if (owner != after)
        pt_property(&j, "issue", "clipboard owner changed during capture");
    pt_put(&j, ",\"captured_ns\":");
    pt_number(&j, pt_now());
    pt_put(&j, "}");
    pt_enqueue(pt_finish(&j));
}

static void recorded(xcb_record_enable_context_reply_t *r) {
    if (r->category != 0 || r->client_swapped)
        return;
    int bytes = xcb_record_enable_context_data_length(r);
    const uint8_t *data = xcb_record_enable_context_data(r);
    for (int offset = 0; offset + 32 <= bytes; offset += 32) {
        xcb_key_press_event_t event;
        memcpy(&event, data + offset, sizeof(event));
        int type = event.response_type & 0x7f;
        if (type == XCB_BUTTON_PRESS && event.detail == 2)
            capture("middle_click", "button2", XCB_ATOM_PRIMARY);
        if (type != XCB_KEY_PRESS || (event.state & (XCB_MOD_MASK_1 | XCB_MOD_MASK_4)))
            continue;
        xcb_get_keyboard_mapping_reply_t *mapping =
            reply(control, xcb_get_keyboard_mapping(control, event.detail, 1).sequence);
        if (!mapping)
            continue;
        xcb_keysym_t key =
            mapping->keysyms_per_keycode ? xcb_get_keyboard_mapping_keysyms(mapping)[0] : 0;
        free(mapping);
        if ((key == 'v' || key == 'V') && (event.state & XCB_MOD_MASK_CONTROL))
            capture("keyboard", event.state & XCB_MOD_MASK_SHIFT ? "ctrl+shift+v" : "ctrl+v",
                    atom("CLIPBOARD"));
        else if (key == 0xff63 && (event.state & XCB_MOD_MASK_SHIFT) &&
                 !(event.state & XCB_MOD_MASK_CONTROL))
            capture("keyboard", "shift+insert", atom("CLIPBOARD"));
    }
}

int pt_open(int limit) {
    pt_reset();
    pt_limit = limit;
    if (is_wayland()) {
        snprintf(pt_failure, sizeof(pt_failure),
                 "native Wayland capture is unavailable; run pastetrail doctor");
        return 0;
    }
    int screen_number = 0;
    control = xcb_connect(NULL, &screen_number);
    events = xcb_connect(NULL, NULL);
    if (xcb_connection_has_error(control) || xcb_connection_has_error(events))
        goto failed;
    xcb_screen_iterator_t screens = xcb_setup_roots_iterator(xcb_get_setup(control));
    while (screen_number-- > 0 && screens.rem)
        xcb_screen_next(&screens);
    if (!screens.rem)
        goto failed;
    root = screens.data->root;
    xcb_record_query_version_reply_t *version =
        reply(control, xcb_record_query_version(control, 1, 13).sequence);
    if (!version)
        goto failed;
    free(version);
    request_window = xcb_generate_id(control);
    xcb_create_window(control, 0, request_window, root, 0, 0, 1, 1, 0, XCB_WINDOW_CLASS_INPUT_ONLY,
                      0, 0, NULL);
    recording = xcb_generate_id(control);
    xcb_record_range_t range = {0};
    range.device_events.first = XCB_KEY_PRESS;
    range.device_events.last = XCB_BUTTON_RELEASE;
    xcb_record_client_spec_t clients = XCB_RECORD_CS_ALL_CLIENTS;
    xcb_record_create_context(control, recording, 0, 1, 1, &clients, &range);
    /* Order creation on the control connection before enabling on the other. */
    void *sync = reply(control, xcb_get_input_focus(control).sequence);
    if (!sync)
        goto failed;
    free(sync);
    record_sequence = xcb_record_enable_context(events, recording).sequence;
    xcb_record_enable_context_reply_t *start = reply(events, record_sequence);
    if (!start)
        goto failed;
    int started = start->category == 4; /* StartOfData */
    free(start);
    if (!started)
        goto failed;
    return 1;
failed:
    pt_close();
    snprintf(
        pt_failure, sizeof(pt_failure),
        "cannot start X11 RECORD; check DISPLAY, authorization, and the server's RECORD extension");
    return 0;
}

char *pt_next(int timeout_ms) {
    char *pending = pt_dequeue();
    if (pending)
        return pending;
    int64_t deadline = monotonic_ms() + timeout_ms;
    do {
        void *data = NULL;
        xcb_generic_error_t *error = NULL;
        if (xcb_poll_for_reply(events, record_sequence, &data, &error)) {
            if (data)
                recorded(data);
            if (error || !data)
                snprintf(pt_failure, sizeof(pt_failure), "X11 RECORD stream ended");
            free(data);
            free(error);
            break;
        }
        if (xcb_connection_has_error(control) || xcb_connection_has_error(events)) {
            snprintf(pt_failure, sizeof(pt_failure), "X11 connection closed");
            break;
        }
        wait_input(events, 1);
    } while (monotonic_ms() < deadline);
    return pt_dequeue();
}

void pt_close(void) {
    if (control) {
        if (recording)
            xcb_record_disable_context(control, recording);
        if (recording)
            xcb_record_free_context(control, recording);
        if (request_window)
            xcb_destroy_window(control, request_window);
        xcb_flush(control);
    }
    if (events)
        xcb_disconnect(events);
    if (control)
        xcb_disconnect(control);
    control = events = NULL;
    recording = request_window = record_sequence = 0;
    pt_reset();
}
