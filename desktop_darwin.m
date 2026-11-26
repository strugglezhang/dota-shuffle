//go:build cgo

#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#include "_cgo_export.h"

@interface DotaApp : NSObject <NSApplicationDelegate, WKURLSchemeHandler, WKScriptMessageHandlerWithReply>
@end

@implementation DotaApp {
    NSWindow *_window;
}

- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    NSRect work = [[NSScreen mainScreen] visibleFrame];
    NSRect content = NSMakeRect(0, 0, MIN(1440, NSWidth(work) * 0.94),
                                      MIN(900, NSHeight(work) * 0.90));
    _window = [[NSWindow alloc] initWithContentRect:content
                                        styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
                                                  NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable
                                          backing:NSBackingStoreBuffered
                                            defer:NO];
    [_window setTitle:@"DOTA 内战局"];
    [_window setContentMinSize:NSMakeSize(340, 320)];
    [_window setReleasedWhenClosed:NO];

    WKWebViewConfiguration *config = [[[WKWebViewConfiguration alloc] init] autorelease];
    [config setURLSchemeHandler:self forURLScheme:@"dotashuffle"];
    WKUserContentController *controller = [config userContentController];
    [controller addScriptMessageHandlerWithReply:self contentWorld:[WKContentWorld pageWorld] name:@"dotaAPI"];
    NSString *bridge =
        @"const browserFetch = window.fetch.bind(window);"
         "window.fetch = async (path, options = {}) => {"
         "  if (typeof path !== 'string' || !path.startsWith('/api/')) return browserFetch(path, options);"
         "  const reply = await window.webkit.messageHandlers.dotaAPI.postMessage("
         "    { path, method: options.method || 'GET', body: options.body || '' });"
         "  return new Response(reply.status === 204 ? null : reply.body, {status: reply.status});"
         "};";
    WKUserScript *script = [[[WKUserScript alloc] initWithSource:bridge
                                                   injectionTime:WKUserScriptInjectionTimeAtDocumentStart
                                                forMainFrameOnly:YES] autorelease];
    [controller addUserScript:script];

    WKWebView *view = [[[WKWebView alloc] initWithFrame:content configuration:config] autorelease];
    [_window setContentView:view];
    [_window center];
    [_window makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
    [view loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:@"dotashuffle://app/"]]];
}

- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)sender {
    return YES;
}

- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
    [sender stop:nil];
    NSEvent *wake = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
                                      location:NSZeroPoint modifierFlags:0 timestamp:0 windowNumber:0
                                       context:nil subtype:0 data1:0 data2:0];
    [sender postEvent:wake atStart:YES];
    return NSTerminateCancel;
}

- (void)webView:(WKWebView *)webView startURLSchemeTask:(id<WKURLSchemeTask>)task {
    NSURL *url = [[task request] URL];
    if (![[url host] isEqualToString:@"app"]) {
        [task didFailWithError:[NSError errorWithDomain:NSURLErrorDomain code:NSURLErrorBadURL userInfo:nil]];
        return;
    }
    NSString *path = [[NSURLComponents componentsWithURL:url resolvingAgainstBaseURL:NO] percentEncodedPath];
    if ([path length] == 0) {
        path = @"/";
    }
    DotaResponse result = DotaHandleRequest("GET", (char *)[path UTF8String], NULL);
    NSString *type = result.content_type ? [NSString stringWithUTF8String:result.content_type] : @"text/plain";
    NSData *data = [NSData dataWithBytes:result.body length:result.size];
    NSHTTPURLResponse *response = [[[NSHTTPURLResponse alloc] initWithURL:url
        statusCode:result.status HTTPVersion:@"HTTP/1.1" headerFields:@{@"Content-Type": type}] autorelease];
    free(result.content_type);
    free(result.body);
    [task didReceiveResponse:response];
    [task didReceiveData:data];
    [task didFinish];
}

- (void)webView:(WKWebView *)webView stopURLSchemeTask:(id<WKURLSchemeTask>)task {
    // The scheme handler finishes each resource synchronously.
}

- (void)userContentController:(WKUserContentController *)controller
      didReceiveScriptMessage:(WKScriptMessage *)message
                replyHandler:(void (^)(id, NSString *))replyHandler {
    id request = [message body];
    if (![[message frameInfo] isMainFrame] ||
        ![[[[message frameInfo] request] URL].scheme isEqualToString:@"dotashuffle"] ||
        ![request isKindOfClass:[NSDictionary class]] ||
        ![request[@"path"] isKindOfClass:[NSString class]] || ![request[@"path"] hasPrefix:@"/api/"] ||
        ![request[@"method"] isKindOfClass:[NSString class]] || ![request[@"body"] isKindOfClass:[NSString class]]) {
        replyHandler(nil, @"无效的本地应用请求");
        return;
    }
    DotaResponse result = DotaHandleRequest((char *)[request[@"method"] UTF8String],
                                            (char *)[request[@"path"] UTF8String],
                                            (char *)[request[@"body"] UTF8String]);
    NSString *body = result.size ? [[[NSString alloc] initWithBytes:result.body length:result.size
                                                            encoding:NSUTF8StringEncoding] autorelease] : @"";
    free(result.content_type);
    free(result.body);
    replyHandler(@{@"status": @(result.status), @"body": body ?: @""}, nil);
}
@end

void DotaRunApp(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        NSMenu *bar = [[[NSMenu alloc] init] autorelease];
        NSMenuItem *appItem = [[[NSMenuItem alloc] init] autorelease];
        [bar addItem:appItem];
        NSMenu *menu = [[[NSMenu alloc] initWithTitle:@"DOTA 内战局"] autorelease];
        [menu addItemWithTitle:@"退出 DOTA 内战局" action:@selector(terminate:) keyEquivalent:@"q"];
        [appItem setSubmenu:menu];
        [NSApp setMainMenu:bar];
        DotaApp *delegate = [[DotaApp alloc] init];
        [NSApp setDelegate:delegate];
        [NSApp run];
        [NSApp setDelegate:nil];
        [delegate release];
    }
}

void DotaShowError(const char *message) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        NSAlert *alert = [[[NSAlert alloc] init] autorelease];
        [alert setAlertStyle:NSAlertStyleCritical];
        [alert setMessageText:@"DOTA 内战局启动失败"];
        [alert setInformativeText:[NSString stringWithUTF8String:message]];
        [alert runModal];
    }
}
