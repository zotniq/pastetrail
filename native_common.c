//go:build cgo && (darwin || linux || windows)

#include "native.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#ifdef _WIN32
#include <windows.h>
#else
#include <time.h>
#endif

int pt_limit;
char pt_failure[256];
static uint64_t sequence, dropped;
static char *queue[64];
static unsigned head, count;
static size_t queued_bytes, remaining_bytes;

const char *pt_error(void) { return pt_failure; }

static void append(pt_json *j, const char *data, size_t n) {
    if (j->failed)
        return;
    if (n > SIZE_MAX - j->len - 1) {
        j->failed = 1;
        return;
    }
    size_t need = j->len + n + 1;
    if (need > j->cap) {
        size_t cap = need > j->cap * 2 ? need : j->cap * 2;
        char *p = (char *)realloc(j->data, cap);
        if (!p) {
            j->failed = 1;
            return;
        }
        j->data = p;
        j->cap = cap;
    }
    memcpy(j->data + j->len, data, n);
    j->len += n;
    j->data[j->len] = 0;
}

void pt_json_init(pt_json *j) { memset(j, 0, sizeof(*j)); }
void pt_put(pt_json *j, const char *s) { append(j, s, strlen(s)); }
void pt_string(pt_json *j, const char *s) {
    pt_put(j, "\"");
    if (s)
        for (const unsigned char *p = (const unsigned char *)s; *p; p++) {
            if (*p == '"' || *p == '\\') {
                char escaped[2] = {'\\', (char)*p};
                append(j, escaped, 2);
            } else if (*p < 32) {
                char escaped[7];
                snprintf(escaped, sizeof(escaped), "\\u%04x", *p);
                pt_put(j, escaped);
            } else
                append(j, (const char *)p, 1);
        }
    pt_put(j, "\"");
}
void pt_number(pt_json *j, int64_t n) {
    char s[32];
    snprintf(s, sizeof(s), "%lld", (long long)n);
    pt_put(j, s);
}
void pt_property(pt_json *j, const char *name, const char *value) {
    pt_put(j, ",");
    pt_string(j, name);
    pt_put(j, ":");
    pt_string(j, value);
}

void pt_format(pt_json *j, const char *type, const void *bytes, size_t size, int *first) {
    static const char alphabet[] =
        "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    size_t n = size > remaining_bytes ? remaining_bytes : size;
    remaining_bytes -= n;
    const unsigned char *data = (const unsigned char *)bytes;
    if (!*first)
        pt_put(j, ",");
    *first = 0;
    pt_put(j, "{\"type\":");
    pt_string(j, type);
    pt_put(j, ",\"size\":");
    pt_number(j, (int64_t)size);
    pt_put(j, ",\"truncated\":");
    pt_put(j, n < size ? "true" : "false");
    pt_put(j, ",\"data\":\"");
    for (size_t i = 0; i < n; i += 3) {
        uint32_t v = (uint32_t)data[i] << 16;
        if (i + 1 < n)
            v |= (uint32_t)data[i + 1] << 8;
        if (i + 2 < n)
            v |= data[i + 2];
        char out[4] = {alphabet[(v >> 18) & 63], alphabet[(v >> 12) & 63],
                       i + 1 < n ? alphabet[(v >> 6) & 63] : '=',
                       i + 2 < n ? alphabet[v & 63] : '='};
        append(j, out, 4);
    }
    pt_put(j, "\"}");
}

char *pt_finish(pt_json *j) {
    if (j->failed) {
        free(j->data);
        snprintf(pt_failure, sizeof(pt_failure), "native event allocation failed");
        return NULL;
    }
    return j->data;
}

int64_t pt_now(void) {
#ifdef _WIN32
    FILETIME ft;
    GetSystemTimeAsFileTime(&ft);
    uint64_t ticks = ((uint64_t)ft.dwHighDateTime << 32) | ft.dwLowDateTime;
    return (int64_t)((ticks - 116444736000000000ULL) * 100);
#else
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    return (int64_t)ts.tv_sec * 1000000000LL + ts.tv_nsec;
#endif
}

void pt_event_begin(pt_json *j, const char *kind, const char *action, const char *source) {
    remaining_bytes = (size_t)pt_limit;
    char id[32];
    snprintf(id, sizeof(id), "%llu", (unsigned long long)++sequence);
    pt_json_init(j);
    pt_put(j, "{\"key\":");
    pt_string(j, id);
    pt_put(j, ",\"time_ns\":");
    pt_number(j, pt_now());
    pt_property(j, "kind", kind);
    pt_property(j, "action", action);
    pt_property(j, "source", source);
}

void pt_enqueue(char *json) {
    if (!json)
        return;
    size_t bytes = strlen(json);
    if (count == 64 || (count && bytes + queued_bytes > 32u * 1024u * 1024u)) {
        free(json);
        dropped++;
        return;
    }
    queued_bytes += bytes;
    queue[(head + count) % 64] = json;
    count++;
}
char *pt_dequeue(void) {
    if (!count)
        return NULL;
    char *p = queue[head];
    queue[head] = NULL;
    head = (head + 1) % 64;
    count--;
    size_t bytes = strlen(p);
    queued_bytes -= bytes;
    if (dropped && bytes && p[bytes - 1] == '}') {
        pt_json j;
        pt_json_init(&j);
        append(&j, p, bytes - 1);
        pt_put(&j, ",\"dropped\":");
        pt_number(&j, (int64_t)dropped);
        pt_put(&j, "}");
        free(p);
        p = pt_finish(&j);
        dropped = 0;
    }
    return p;
}
void pt_reset(void) {
    dropped = 0;
    char *p;
    while ((p = pt_dequeue()))
        free(p);
    head = 0;
    queued_bytes = 0;
    pt_failure[0] = 0;
}
