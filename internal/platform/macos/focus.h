/* Focus metadata is sampled in the native input callback, before clipboard
 * reads and Go enrichment. Never fetch AXValue or AXSelectedText. */
#include <time.h>

typedef AXError (*focus_attribute_reader)(AXUIElementRef, CFStringRef, CFTypeRef *);
typedef AXError (*focus_attributes_reader)(AXUIElementRef, CFArrayRef,
                                         AXCopyMultipleAttributeOptions, CFArrayRef *);

static double focus_time(void) {
    struct timespec now;
    clock_gettime(CLOCK_MONOTONIC, &now);
    return now.tv_sec + now.tv_nsec / 1e9;
}

static void focus_unavailable(pt_json *j, const char *reason) {
    pt_put(j, ",\"focus\":{\"issue\":");
    pt_string(j, reason);
    pt_put(j, "}");
}

static NSString *focus_string(CFArrayRef values, CFIndex index) {
    CFTypeRef value = CFArrayGetValueAtIndex(values, index);
    if (CFGetTypeID(value) == CFStringGetTypeID())
        return (__bridge NSString *)value;
    if (CFGetTypeID(value) == CFURLGetTypeID())
        return [(__bridge NSURL *)value absoluteString];
    return nil;
}

static void focus_property(pt_json *j, const char *key, NSString *value, const char **issue) {
    if ([value length] > 4096) {
        *issue = "oversized accessibility metadata omitted";
        return;
    }
    if ([value length])
        pt_property(j, key, [value UTF8String]);
}

static void focus_context(pt_json *j, AXUIElementRef application,
                          focus_attribute_reader read, focus_attributes_reader read_many) {
    CFTypeRef focused = NULL;
    AXUIElementSetMessagingTimeout(application, 0.01);
    double deadline = focus_time() + 0.05;
    if (read(application, kAXFocusedUIElementAttribute, &focused) != kAXErrorSuccess ||
        !focused || CFGetTypeID(focused) != AXUIElementGetTypeID()) {
        if (focused)
            CFRelease(focused);
        focus_unavailable(j, "application did not expose its focused accessibility control");
        return;
    }
    NSArray *attributes = @[
        (id)kAXRoleAttribute, (id)kAXSubroleAttribute, (id)kAXIdentifierAttribute,
        (id)kAXTitleAttribute, (id)kAXDescriptionAttribute, (id)kAXParentAttribute,
        (id)kAXDocumentAttribute, (id)kAXSelectedTextRangeAttribute, (id)kAXRoleDescriptionAttribute
    ];
    pt_json snapshot;
    pt_json_init(&snapshot);
    pt_put(&snapshot, "{\"path\":[");
    AXUIElementRef element = (AXUIElementRef)CFRetain(focused);
    CFArrayRef leaf = NULL;
    const char *issue = NULL;
    unsigned count = 0;
    while (element && count < 8 && focus_time() < deadline) {
        AXUIElementSetMessagingTimeout(element, 0.01);
        CFArrayRef values = NULL;
        AXError error = read_many(element, (__bridge CFArrayRef)attributes, 0, &values);
        if (error != kAXErrorSuccess || !values || CFArrayGetCount(values) != 9) {
            if (values)
                CFRelease(values);
            issue = "accessibility control metadata could not be read within the capture budget";
            break;
        }
        NSString *role = focus_string(values, 0);
        if ([role isEqualToString:(id)kAXWindowRole] ||
            [role isEqualToString:(id)kAXApplicationRole]) {
            CFRelease(values);
            CFRelease(element);
            element = NULL;
            break;
        }
        if (!count)
            leaf = (CFArrayRef)CFRetain(values);
        if (count++)
            pt_put(&snapshot, ",");
        pt_put(&snapshot, "{\"role\":");
        pt_string(&snapshot, [role length] <= 256 ? [role UTF8String] : "");
        focus_property(&snapshot, "subrole", focus_string(values, 1), &issue);
        focus_property(&snapshot, "identifier", focus_string(values, 2), &issue);
        focus_property(&snapshot, "title", focus_string(values, 3), &issue);
        focus_property(&snapshot, "description", focus_string(values, 4), &issue);
        focus_property(&snapshot, "role_description", focus_string(values, 8), &issue);
        pt_put(&snapshot, "}");
        CFTypeRef parent = CFArrayGetValueAtIndex(values, 5);
        AXUIElementRef next = NULL;
        if (CFGetTypeID(parent) == AXUIElementGetTypeID()) {
            if (CFEqual(parent, element))
                issue = "accessibility parent cycle encountered";
            else
                next = (AXUIElementRef)CFRetain(parent);
        }
        CFRelease(values);
        CFRelease(element);
        element = next;
    }
    if (element) {
        CFRelease(element);
        if (!issue)
            issue = "accessibility ancestry exceeded the depth or time budget";
    }
    pt_put(&snapshot, "]");
    if (leaf) {
        // Only the focused control can supply the destination document. A
        // parent/window may refer to an editor even when a terminal has focus.
        focus_property(&snapshot, "document", focus_string(leaf, 6), &issue);
        CFTypeRef selection = CFArrayGetValueAtIndex(leaf, 7);
        CFRange range;
        if (CFGetTypeID(selection) == AXValueGetTypeID() &&
            AXValueGetType((AXValueRef)selection) == kAXValueCFRangeType &&
            AXValueGetValue((AXValueRef)selection, kAXValueCFRangeType, &range) &&
            range.location >= 0 && range.length >= 0) {
            pt_put(&snapshot, ",\"selection\":{\"start\":");
            pt_number(&snapshot, range.location);
            pt_put(&snapshot, ",\"length\":");
            pt_number(&snapshot, range.length);
            pt_put(&snapshot, "}");
        }
        CFRelease(leaf);
    }
    if (issue)
        pt_property(&snapshot, "issue", issue);
    pt_put(&snapshot, "}");
    char *json = pt_finish(&snapshot);
    // Discard data if focus moved while reading the ancestry.
    CFTypeRef after = NULL;
    bool stable = read(application, kAXFocusedUIElementAttribute, &after) == kAXErrorSuccess &&
                  after && CFEqual(focused, after);
    if (after)
        CFRelease(after);
    CFRelease(focused);
    if (stable && json) {
        pt_put(j, ",\"focus\":");
        pt_put(j, json);
    } else {
        focus_unavailable(j, "focused control changed or could not be confirmed during capture");
    }
    free(json);
}
