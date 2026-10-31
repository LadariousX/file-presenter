//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#include "window_darwin.h"

static NSWindow *window;
static NSString *viewerURL;

// stopApp hides the window and makes [NSApp run] return to Go, which then shuts down cleanly.
static void stopApp(void) {
	[window orderOut:nil];
	[NSApp stop:nil];
	// stop: only takes effect after the next event, so post one.
	[NSApp postEvent:[NSEvent otherEventWithType:NSEventTypeApplicationDefined location:NSZeroPoint
		modifierFlags:0 timestamp:0 windowNumber:0 context:nil subtype:0 data1:0 data2:0] atStart:YES];
}

static void activate(void) {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
	[NSApp activateIgnoringOtherApps:YES];
#pragma clang diagnostic pop
}

@interface FPLink : NSButton
@end
@implementation FPLink
- (void)resetCursorRects { [self addCursorRect:self.bounds cursor:NSCursor.pointingHandCursor]; }
@end

@interface FPDelegate : NSObject <NSApplicationDelegate, NSWindowDelegate>
@end
@implementation FPDelegate
- (void)openViewer:(id)sender { [NSWorkspace.sharedWorkspace openURL:[NSURL URLWithString:viewerURL]]; }
- (void)windowWillClose:(NSNotification *)n { stopApp(); }
// ⌘Q and the Dock's Quit: let Go shut down (closing the viewer tab) instead of exiting right here.
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)app {
	stopApp();
	return NSTerminateCancel;
}
// Clicking the Dock icon brings the window back.
- (BOOL)applicationShouldHandleReopen:(NSApplication *)app hasVisibleWindows:(BOOL)visible {
	[window makeKeyAndOrderFront:nil];
	return YES;
}
@end

static NSTextField *label(NSString *s, NSFont *font, NSColor *color, CGFloat y, CGFloat h) {
	NSTextField *t = [NSTextField labelWithString:s];
	t.font = font;
	t.textColor = color;
	t.frame = NSMakeRect(100, y, 350, h);
	return t;
}

static void setUpApp(const void *icon, int iconLen) {
	[NSApplication sharedApplication];
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
	if (NSBundle.mainBundle.bundleIdentifier == nil && iconLen > 0) // `go run`: no app bundle, no icon
		NSApp.applicationIconImage = [[NSImage alloc] initWithData:[NSData dataWithBytes:icon length:iconLen]];
	// Menu: File Presenter → Close Window (⌘W), Quit File Presenter (⌘Q).
	NSMenu *bar = [NSMenu new], *appMenu = [NSMenu new];
	NSMenuItem *appItem = [NSMenuItem new];
	appItem.submenu = appMenu;
	[bar addItem:appItem];
	[appMenu addItemWithTitle:@"Close Window" action:@selector(performClose:) keyEquivalent:@"w"];
	[appMenu addItemWithTitle:@"Quit File Presenter" action:@selector(terminate:) keyEquivalent:@"q"];
	NSApp.mainMenu = bar;
}

// makeWindow builds the "File Presenter is running" window; delegate handles the link and closing.
static NSWindow *makeWindow(NSImage *icon, FPDelegate *delegate) {
	NSWindow *w = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 470, 150)
		styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable
		backing:NSBackingStoreBuffered defer:NO];
	w.title = @"File Presenter";
	w.releasedWhenClosed = NO;
	w.delegate = delegate;
	NSView *v = w.contentView;

	NSImageView *img = [NSImageView imageViewWithImage:icon];
	img.frame = NSMakeRect(20, 66, 64, 64);
	[v addSubview:img];
	[v addSubview:label(@"File Presenter is running", [NSFont boldSystemFontOfSize:15], NSColor.labelColor, 106, 22)];
	[v addSubview:label(@"A browser window should have opened. If it didn't, click:",
		[NSFont systemFontOfSize:13], NSColor.labelColor, 82, 18)];
	FPLink *link = [FPLink buttonWithTitle:viewerURL target:delegate action:@selector(openViewer:)];
	link.bordered = NO;
	link.attributedTitle = [[NSAttributedString alloc] initWithString:viewerURL attributes:@{
		NSFontAttributeName: [NSFont systemFontOfSize:13],
		NSForegroundColorAttributeName: NSColor.linkColor,
		NSUnderlineStyleAttributeName: @(NSUnderlineStyleSingle),
	}];
	[link sizeToFit];
	[link setFrameOrigin:NSMakePoint(98, 58)];
	[v addSubview:link];
	[v addSubview:label(@"Close this window to stop File Presenter.", [NSFont systemFontOfSize:11],
		NSColor.secondaryLabelColor, 20, 16)];
	return w;
}

void fpRun(const char *url, const void *icon, int iconLen) {
	@autoreleasepool {
		viewerURL = [NSString stringWithUTF8String:url];
		setUpApp(icon, iconLen);
		static FPDelegate *delegate;
		delegate = [FPDelegate new];
		NSApp.delegate = delegate;
		window = makeWindow(NSApp.applicationIconImage, delegate);
		[window center];
		[window makeKeyAndOrderFront:nil];
		activate();
		[NSApp run];
	}
}

void fpClose(void) {
	dispatch_async(dispatch_get_main_queue(), ^{ stopApp(); });
}

void fpAlert(const char *msg) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
		activate();
		NSAlert *a = [NSAlert new];
		a.messageText = @"File Presenter can't start";
		a.informativeText = [NSString stringWithUTF8String:msg];
		[a runModal];
	}
}
