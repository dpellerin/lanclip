#import <AppKit/AppKit.h>

static NSStatusItem *statusItem;
static NSImage *idleImage;
static NSImage *remoteImage;

static NSImage *indicatorImage(BOOL remote) {
    NSImage *image = [NSImage imageWithSize:NSMakeSize(18, 18)
                                   flipped:NO
                            drawingHandler:^BOOL(NSRect bounds) {
        [NSGraphicsContext saveGraphicsState];
        if (remote) {
            NSColor *yellow = [NSColor colorWithSRGBRed:1 green:0.78 blue:0.22 alpha:1];
            NSShadow *glow = [[[NSShadow alloc] init] autorelease];
            glow.shadowColor = [yellow colorWithAlphaComponent:0.9];
            glow.shadowBlurRadius = 3;
            [glow set];
            [yellow setStroke];
        } else {
            [NSColor.blackColor setStroke];
        }
        NSBezierPath *sheets = [NSBezierPath bezierPath];
        sheets.lineWidth = 1.6;
        sheets.lineCapStyle = NSLineCapStyleRound;
        [sheets moveToPoint:NSMakePoint(3.5, 5.8)];
        [sheets lineToPoint:NSMakePoint(3.5, 13.8)];
        [sheets curveToPoint:NSMakePoint(4.7, 15)
                controlPoint1:NSMakePoint(3.5, 14.5)
                controlPoint2:NSMakePoint(4, 15)];
        [sheets lineToPoint:NSMakePoint(12, 15)];
        [sheets appendBezierPathWithRoundedRect:NSMakeRect(6.5, 3, 8, 9)
                                       xRadius:1.2
                                       yRadius:1.2];
        [sheets stroke];
        [NSGraphicsContext restoreGraphicsState];
        return YES;
    }];
    image.template = !remote;
    return image;
}

void lanclip_update_indicator(int remote) {
    dispatch_async(dispatch_get_main_queue(), ^{
        statusItem.button.image = remote ? remoteImage : idleImage;
        statusItem.button.accessibilityLabel = remote ? @"Lanclip, remote clipboard" : @"Lanclip";
    });
}

void lanclip_run_indicator(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        idleImage = indicatorImage(NO);
        remoteImage = indicatorImage(YES);
        statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
        statusItem.button.image = idleImage;
        statusItem.button.accessibilityLabel = @"Lanclip";
        [NSApp run];
    }
}
