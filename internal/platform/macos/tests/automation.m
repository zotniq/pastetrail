#include "../automation.m"
#include <assert.h>
#include <string.h>

static OSStatus permission_status, send_status, reply_status;
static unsigned sends;
static int browser_mode;
static const char *reply_url = "https://example.com/";

static OSStatus fixture_permission(const AEAddressDesc *target, AEEventClass cls, AEEventID event,
                                   Boolean ask) {
    assert(target->descriptorType == typeKernelProcessID);
    int pid = 0;
    assert(AEGetDescData(target, &pid, sizeof(pid)) == noErr && pid == 1353);
    assert(cls == kAECoreSuite && event == kAEGetData && !ask);
    return permission_status;
}

static OSStatus fixture_send(const AppleEvent *event, AppleEvent *reply, AESendMode mode,
                             long timeout) {
    sends++;
    assert((mode & kAENeverInteract) && timeout == 15);
    AEDesc direct = {typeNull, NULL};
    assert(AEGetParamDesc(event, keyDirectObject, typeWildCard, &direct) == noErr);
    NSAppleEventDescriptor *spec =
        [[[NSAppleEventDescriptor alloc] initWithAEDescNoCopy:&direct] autorelease];
    assert([[spec descriptorForKeyword:keyAEKeyData] typeCodeValue] ==
        (browser_mode == 2 ? 'pURL' : 'URL '));
    NSAppleEventDescriptor *session = [spec descriptorForKeyword:keyAEContainer];
    if (browser_mode == 2) {
        assert([[session descriptorForKeyword:keyAEDesiredClass] typeCodeValue] == cDocument);
    } else {
        assert([[session descriptorForKeyword:keyAEKeyData] typeCodeValue] == 'acTa');
    }
    if (send_status)
        return send_status;
    NSAppleEventDescriptor *response =
        [NSAppleEventDescriptor appleEventWithEventClass:kCoreEventClass
                                                 eventID:kAEAnswer
                                        targetDescriptor:[NSAppleEventDescriptor nullDescriptor]
                                                returnID:kAutoGenerateReturnID
                                           transactionID:kAnyTransactionID];
    [response setParamDescriptor:[NSAppleEventDescriptor descriptorWithInt32:reply_status]
                      forKeyword:keyErrorNumber];
    if (reply_url)
        [response
            setParamDescriptor:[NSAppleEventDescriptor
                                   descriptorWithString:[NSString stringWithUTF8String:reply_url]]
                    forKeyword:keyDirectObject];
    return AEDuplicateDesc([response aeDesc], reply);
}

int main(void) {
    @autoreleasepool {
        char url[64];
        for (browser_mode = 1; browser_mode <= 2; browser_mode++) {
            assert(read_app_string(1353, browser_url_spec(browser_mode), url, sizeof(url),
                                   fixture_permission, fixture_send) == noErr);
            assert(strcmp(url, reply_url) == 0);
        }
        puts("browser Apple Events passed");
    }
}
