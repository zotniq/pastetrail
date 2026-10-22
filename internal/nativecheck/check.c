#include "../../native.h"
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static void expect(pt_json *j, const char *want) {
    char *got = pt_finish(j);
    assert(got && strcmp(got, want) == 0);
    free(got);
}

int main(void) {
    pt_reset();
    assert(pt_dequeue() == NULL);
    assert(pt_error()[0] == 0);
    assert(pt_now() > 0);
    pt_json j;
    pt_json_init(&j);
    pt_string(&j, "a\"\\\n\t\001");
    expect(&j, "\"a\\\"\\\\\\u000a\\u0009\\u0001\"");
    pt_json_init(&j);
    pt_string(&j, NULL);
    expect(&j, "\"\"");
    pt_json_init(&j);
    pt_number(&j, INT64_MIN);
    expect(&j, "-9223372036854775808");
    pt_json_init(&j);
    pt_property(&j, "name", "value");
    expect(&j, ",\"name\":\"value\"");

    pt_limit = 3;
    pt_event_begin(&j, "keyboard", "test", "fixture");
    free(pt_finish(&j));
    pt_json_init(&j);
    int first = 1;
    pt_format(&j, "text/plain", "abcd", 4, &first);
    pt_format(&j, "text/html", "ab", 2, &first);
    expect(&j, "{\"type\":\"text/"
               "plain\",\"size\":4,\"truncated\":true,\"data\":\"YWJj\"},{\"type\":\"text/"
               "html\",\"size\":2,\"truncated\":true,\"data\":\"\"}");
    for (int n = 0; n <= 3; n++) {
        const char *expected[] = {"\"data\":\"\"", "\"data\":\"YQ==\"", "\"data\":\"YWI=\"",
                                  "\"data\":\"YWJj\""};
        pt_event_begin(&j, "keyboard", "test", "fixture");
        pt_put(&j, ",\"formats\":[");
        first = 1;
        pt_format(&j, "text/plain", "abc", (size_t)n, &first);
        pt_put(&j, "]}");
        char *value = pt_finish(&j);
        assert(strstr(value, expected[n]));
        free(value);
    }
    pt_json_init(&j);
    j.failed = 1;
    pt_put(&j, "ignored");
    assert(pt_finish(&j) == NULL);
    assert(strstr(pt_error(), "allocation"));
    pt_reset();
    pt_enqueue(NULL);
    for (int i = 0; i < 67; i++)
        pt_enqueue(strdup("{\"fixture\":true}"));
    char *value = pt_dequeue();
    assert(value && strstr(value, "\"dropped\":3"));
    free(value);
    for (int i = 1; i < 64; i++) {
        value = pt_dequeue();
        assert(value);
        free(value);
    }
    assert(pt_dequeue() == NULL);
    pt_enqueue(strdup("{}"));
    pt_reset();
    assert(pt_dequeue() == NULL);
    puts("native serialization, byte limits, queue overflow, and cleanup passed");
    return 0;
}
