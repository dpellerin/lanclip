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
        NSBezierPath *letters = [NSBezierPath bezierPath];
        letters.lineWidth = 1.8;
        letters.lineCapStyle = NSLineCapStyleRound;
        letters.lineJoinStyle = NSLineJoinStyleRound;
        [letters moveToPoint:NSMakePoint(3.5, 12.8)];
        [letters lineToPoint:NSMakePoint(3.5, 5.2)];
        [letters lineToPoint:NSMakePoint(7.2, 5.2)];
        [letters moveToPoint:NSMakePoint(14.8, 11.8)];
        [letters curveToPoint:NSMakePoint(14.8, 6.2)
                controlPoint1:NSMakePoint(7.4, 16.8)
                controlPoint2:NSMakePoint(7.4, 1.2)];
        [letters stroke];
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
