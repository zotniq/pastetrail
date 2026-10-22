#ifndef PASTETRAIL_NATIVE_H
#define PASTETRAIL_NATIVE_H
#include <stddef.h>
#include <stdint.h>

const char *pt_probe(void);
int pt_open(int max_bytes);
char *pt_next(int timeout_ms);
void pt_close(void);
const char *pt_error(void);

/* The native pump owns this state on a single locked thread. */
extern int pt_limit;
extern char pt_failure[256];

typedef struct {
    char *data;
    size_t len, cap;
    int failed;
} pt_json;
void pt_json_init(pt_json *j);
void pt_put(pt_json *j, const char *s);
void pt_string(pt_json *j, const char *s);
void pt_number(pt_json *j, int64_t value);
void pt_property(pt_json *j, const char *name, const char *value);
void pt_format(pt_json *j, const char *type, const void *data, size_t size, int *first);
char *pt_finish(pt_json *j);
int64_t pt_now(void);
void pt_event_begin(pt_json *j, const char *kind, const char *action, const char *source);
void pt_enqueue(char *json);
char *pt_dequeue(void);
void pt_reset(void);
#endif
