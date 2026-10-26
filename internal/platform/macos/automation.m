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

static NSAppleEventDescriptor *first_element_spec(OSType type) {
    NSAppleEventDescriptor *window = [NSAppleEventDescriptor recordDescriptor];
    [window setDescriptor:[NSAppleEventDescriptor descriptorWithTypeCode:type]
               forKeyword:keyAEDesiredClass];
    [window setDescriptor:[NSAppleEventDescriptor descriptorWithEnumCode:formAbsolutePosition]
               forKeyword:keyAEKeyForm];
    [window setDescriptor:[NSAppleEventDescriptor descriptorWithInt32:1] forKeyword:keyAEKeyData];
    [window setDescriptor:[NSAppleEventDescriptor nullDescriptor] forKeyword:keyAEContainer];
    return [window coerceToDescriptorType:typeObjectSpecifier];
}

static NSAppleEventDescriptor *browser_url_spec(int kind) {
    if (kind == 2)
        return property_spec('pURL', first_element_spec(cDocument));
    return property_spec('URL ', property_spec('acTa', first_element_spec(cWindow)));
}

// Address the captured PID, never a name that could launch or select another app.
// Browser queries run outside the input tap.
static int read_app_string(int pid, NSAppleEventDescriptor *spec, char *output, size_t size,
                           OSStatus (*permission)(const AEAddressDesc *, AEEventClass, AEEventID,
                                                  Boolean),
                           OSStatus (*send)(const AppleEvent *, AppleEvent *, AESendMode, long)) {
    if (size)
        output[0] = 0;
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
        [request setParamDescriptor:spec forKeyword:keyDirectObject];
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
                snprintf(output, size, "%s", [value UTF8String]);
            }
        } else {
            AEDisposeDesc(&reply);
        }
        return status;
    }
}

int pt_browser_url(int pid, int kind, char *url, size_t size) {
    @autoreleasepool {
        return read_app_string(pid, browser_url_spec(kind), url, size,
                               AEDeterminePermissionToAutomateTarget, AESendMessage);
    }
}
