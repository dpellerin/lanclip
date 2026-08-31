#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

long lanclip_change_count(void) {
    @autoreleasepool { return [[NSPasteboard generalPasteboard] changeCount]; }
}

char *lanclip_read_string(void) {
    @autoreleasepool {
        NSString *value = [[NSPasteboard generalPasteboard] stringForType:NSPasteboardTypeString];
        if (value == nil) return NULL;
        const char *utf8 = [value UTF8String];
        return utf8 == NULL ? NULL : strdup(utf8);
    }
}

int lanclip_write_string(const char *value) {
    @autoreleasepool {
        NSPasteboard *board = [NSPasteboard generalPasteboard];
        NSString *text = [NSString stringWithUTF8String:value];
        if (text == nil) return 0;
        [board clearContents];
        return [board setString:text forType:NSPasteboardTypeString] ? 1 : 0;
    }
}
