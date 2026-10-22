//go:build darwin && cgo

#import <AppKit/AppKit.h>
#import <CoreServices/CoreServices.h>
#include <stdio.h>

static NSAppleEventDescriptor *property_spec(OSType property, NSAppleEventDescriptor *container) {
    NSAppleEventDescriptor *spec = [NSAppleEventDescriptor recordDescriptor];
    [spec setDescriptor:[NSAppleEventDescriptor descriptorWithTypeCode:cProperty]
             forKeyword:keyAEDesiredClass];
    [spec setDescriptor:[NSAppleEventDescriptor descriptorWithEnumCode:formPropertyID]
             forKeyword:keyAEKeyForm];
    [spec setDescriptor:[NSAppleEventDescriptor descriptorWithTypeCode:property]
             forKeyword:keyAEKeyData];
    [spec setDescriptor:container forKeyword:keyAEContainer];
    return [spec coerceToDescriptorType:typeObjectSpecifier];
}

static NSAppleEventDescriptor *terminal_tty_spec(bool iterm) {
    NSAppleEventDescriptor *root = [NSAppleEventDescriptor nullDescriptor];
    if (iterm) {
        NSAppleEventDescriptor *window = property_spec('Crwn', root);
        return property_spec('Stty', property_spec('Wcsn', window));
    }
    NSAppleEventDescriptor *window = [NSAppleEventDescriptor recordDescriptor];
    [window setDescriptor:[NSAppleEventDescriptor descriptorWithTypeCode:cWindow]
               forKeyword:keyAEDesiredClass];
    [window setDescriptor:[NSAppleEventDescriptor descriptorWithEnumCode:formAbsolutePosition]
               forKeyword:keyAEKeyForm];
    [window setDescriptor:[NSAppleEventDescriptor descriptorWithInt32:1] forKeyword:keyAEKeyData];
    [window setDescriptor:root forKeyword:keyAEContainer];
    return property_spec(
        'ttty', property_spec('tcnt', [window coerceToDescriptorType:typeObjectSpecifier]));
}

// Address the captured PID, never a name that could launch or select another app.
// These are read-only Apple Events on the enrichment thread, outside the input tap.
static int read_terminal_tty(int pid, bool iterm, char *tty, size_t size,
                             OSStatus (*permission)(const AEAddressDesc *, AEEventClass, AEEventID,
                                                    Boolean),
                             OSStatus (*send)(const AppleEvent *, AppleEvent *, AESendMode, long)) {
    if (size)
        tty[0] = 0;
    if (pid <= 0 || size == 0)
        return paramErr;
    @autoreleasepool {
        NSAppleEventDescriptor *target =
            [NSAppleEventDescriptor descriptorWithDescriptorType:typeKernelProcessID
                                                           bytes:&pid
                                                          length:sizeof(pid)];
        OSStatus status = permission([target aeDesc], kAECoreSuite, kAEGetData, false);
        if (status != noErr)
            return status;
        NSAppleEventDescriptor *request =
            [NSAppleEventDescriptor appleEventWithEventClass:kAECoreSuite
                                                     eventID:kAEGetData
                                            targetDescriptor:target
                                                    returnID:kAutoGenerateReturnID
                                               transactionID:kAnyTransactionID];
        [request setParamDescriptor:terminal_tty_spec(iterm) forKeyword:keyDirectObject];
        AppleEvent reply = {typeNull, NULL};
        status =
            send([request aeDesc], &reply, kAEWaitReply | kAENeverInteract | kAEDontRecord, 15);
        if (status == noErr) {
            NSAppleEventDescriptor *response =
                [[[NSAppleEventDescriptor alloc] initWithAEDescNoCopy:&reply] autorelease];
            status = [[response paramDescriptorForKeyword:keyErrorNumber] int32Value];
            NSString *value = [[response paramDescriptorForKeyword:keyDirectObject] stringValue];
            if (status == noErr) {
                if (!value || [value lengthOfBytesUsingEncoding:NSUTF8StringEncoding] >= size)
                    return errAECoercionFail;
                snprintf(tty, size, "%s", [value UTF8String]);
            }
        } else {
            AEDisposeDesc(&reply);
        }
        return status;
    }
}

int pt_terminal_tty(int pid, int iterm, char *tty, size_t size) {
    return read_terminal_tty(pid, iterm != 0, tty, size, AEDeterminePermissionToAutomateTarget,
                             AESendMessage);
}
