/* Deterministic AX fixtures: no permission prompt, real input, or clipboard. */
static AXUIElementRef focus_leaf, focus_parent;
static NSArray *focus_leaf_attributes, *focus_parent_attributes;
static unsigned focus_read_count;
static bool focus_moves, focus_missing, focus_bad_attributes;

static AXError fixture_focus_attribute(AXUIElementRef app, CFStringRef attribute, CFTypeRef *out) {
    (void)app;
    assert(CFEqual(attribute, kAXFocusedUIElementAttribute));
    if (focus_missing)
        return kAXErrorNoValue;
    *out = CFRetain(focus_moves && focus_read_count++ ? focus_parent : focus_leaf);
    return kAXErrorSuccess;
}

static AXError fixture_focus_attributes(AXUIElementRef element, CFArrayRef attributes,
                                        AXCopyMultipleAttributeOptions options, CFArrayRef *out) {
    (void)options;
    // Capture must never read editor/terminal contents or the selected text.
    for (CFIndex i = 0; i < CFArrayGetCount(attributes); i++) {
        CFTypeRef attribute = CFArrayGetValueAtIndex(attributes, i);
        assert(!CFEqual(attribute, kAXValueAttribute));
        assert(!CFEqual(attribute, kAXSelectedTextAttribute));
    }
    if (focus_bad_attributes)
        return kAXErrorCannotComplete;
    *out = (CFArrayRef)CFRetain((__bridge CFArrayRef)(CFEqual(element, focus_leaf)
                                                    ? focus_leaf_attributes
                                                    : focus_parent_attributes));
    return kAXErrorSuccess;
}

static NSDictionary *fixture_focus_snapshot(void) {
    focus_read_count = 0;
    pt_json j;
    pt_json_init(&j);
    pt_put(&j, "{\"fixture\":true");
    focus_context(&j, focus_parent, fixture_focus_attribute, fixture_focus_attributes);
    pt_put(&j, "}");
    char *json = pt_finish(&j);
    assert(json);
    NSData *data = [NSData dataWithBytes:json length:strlen(json)];
    free(json);
    NSDictionary *result = [NSJSONSerialization JSONObjectWithData:data options:0 error:NULL];
    assert(result);
    return result[@"focus"];
}

static void check_focus_metadata(void) {
    focus_leaf = AXUIElementCreateApplication(2147483001);
    focus_parent = AXUIElementCreateApplication(2147483002);
    CFRange range = CFRangeMake(0, 0);
    AXValueRef selection = AXValueCreate(kAXValueCFRangeType, &range);
    id absent = [NSNull null];
    focus_leaf_attributes = @[
        @"AXTextArea", absent, @"editor", @"main.go", absent, (__bridge id)focus_parent,
        [NSURL URLWithString:@"file:///project/main.go"], (__bridge id)selection, @"editor"
    ];
    focus_parent_attributes = @[
        @"AXWindow", absent, absent, @"inactive.go", absent, absent,
        @"file:///inactive.go", absent, absent
    ];
    NSDictionary *snapshot = fixture_focus_snapshot();
    assert([snapshot[@"path"] count] == 1);
    assert([snapshot[@"document"] isEqualToString:@"file:///project/main.go"]);
    assert([snapshot[@"selection"][@"start"] isEqual:@0]);
    assert([snapshot[@"path"][0][@"role_description"] isEqualToString:@"editor"]);
    assert(!snapshot[@"issue"]);

    focus_leaf_attributes = @[
        @"AXTextArea", absent, absent, @"Terminal 1, zsh", absent,
        (__bridge id)focus_parent, absent, absent, absent
    ];
    snapshot = fixture_focus_snapshot();
    assert(!snapshot[@"document"] && !snapshot[@"selection"]);
    assert([snapshot[@"path"] count] == 1); // Window title/document are excluded.

    focus_moves = true;
    snapshot = fixture_focus_snapshot();
    assert(snapshot[@"issue"] && !snapshot[@"path"] && !snapshot[@"document"]);
    focus_moves = false;
    focus_missing = true;
    snapshot = fixture_focus_snapshot();
    assert(snapshot[@"issue"] && !snapshot[@"path"]);
    focus_missing = false;
    focus_bad_attributes = true;
    snapshot = fixture_focus_snapshot();
    assert(snapshot[@"issue"] && [snapshot[@"path"] count] == 0);
    focus_bad_attributes = false;

    // A broken ancestry must be bounded, preserving already sampled metadata.
    focus_parent_attributes = @[
        @"AXGroup", absent, absent, @"Terminal", absent,
        (__bridge id)focus_leaf, absent, absent, absent
    ];
    snapshot = fixture_focus_snapshot();
    assert(snapshot[@"issue"] && [snapshot[@"path"] count] <= 8);

    CFRelease(selection);
    CFRelease(focus_leaf);
    CFRelease(focus_parent);
    focus_leaf_attributes = focus_parent_attributes = nil;
}
