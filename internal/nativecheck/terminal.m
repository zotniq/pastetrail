#include "../../native_terminal_darwin.m"
#include <assert.h>
#include <string.h>

static OSStatus permission_status, send_status, reply_status;
static unsigned sends;
static bool expect_iterm;
static const char *reply_tty = "/dev/ttys003";

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
           (expect_iterm ? 'Stty' : 'ttty'));
    NSAppleEventDescriptor *session = [spec descriptorForKeyword:keyAEContainer];
    assert([[session descriptorForKeyword:keyAEKeyData] typeCodeValue] ==
           (expect_iterm ? 'Wcsn' : 'tcnt'));
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
    if (reply_tty)
        [response
            setParamDescriptor:[NSAppleEventDescriptor
                                   descriptorWithString:[NSString stringWithUTF8String:reply_tty]]
                    forKeyword:keyDirectObject];
    return AEDuplicateDesc([response aeDesc], reply);
}

int main(void) {
    @autoreleasepool {
        char tty[64];
        for (int kind = 0; kind < 2; kind++) {
            expect_iterm = kind;
            assert(read_terminal_tty(1353, kind, tty, sizeof(tty), fixture_permission,
                                     fixture_send) == 0);
            assert(strcmp(tty, reply_tty) == 0);
        }
        assert(sends == 2);
        permission_status = errAEEventNotPermitted;
        assert(read_terminal_tty(1353, true, tty, sizeof(tty), fixture_permission, fixture_send) ==
                   permission_status &&
               !tty[0]);
        assert(sends == 2);
        permission_status = 0;
        send_status = errAETimeout;
        assert(read_terminal_tty(1353, true, tty, sizeof(tty), fixture_permission, fixture_send) ==
               send_status);
        send_status = 0;
        reply_status = errAENoSuchObject;
        assert(read_terminal_tty(1353, true, tty, sizeof(tty), fixture_permission, fixture_send) ==
               reply_status);
        reply_status = 0;
        reply_tty = NULL;
        assert(read_terminal_tty(1353, true, tty, sizeof(tty), fixture_permission, fixture_send) ==
               errAECoercionFail);
        reply_tty = "/dev/ttys003";
        assert(read_terminal_tty(1353, true, tty, 2, fixture_permission, fixture_send) ==
               errAECoercionFail);
        assert(read_terminal_tty(0, true, tty, sizeof(tty), fixture_permission, fixture_send) ==
               paramErr);
        assert(read_terminal_tty(1353, true, tty, 0, fixture_permission, fixture_send) == paramErr);
        puts("terminal session Apple Events, permissions, errors, and bounds passed");
    }
}
