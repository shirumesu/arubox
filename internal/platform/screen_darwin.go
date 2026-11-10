package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework CoreGraphics -framework ScreenCaptureKit -framework Vision
#import <Cocoa/Cocoa.h>
#import <CoreGraphics/CoreGraphics.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <Vision/Vision.h>
#include <unistd.h>

typedef struct {
	char *text;
	double x, y, width, height;
} textLine;

enum {
	screenOK = 0,
	screenDenied = 1,
	screenFailed = 2,
};

static bool screenCaptureAllowed(void) {
	return CGPreflightScreenCaptureAccess();
}

static void requestScreenCapture(void) {
	CGRequestScreenCaptureAccess();
}

static void openScreenCaptureSettings(void) {
	NSURL *url = [NSURL URLWithString:@"x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"];
	[[NSWorkspace sharedWorkspace] openURL:url];
}

// captureRect takes a picture of a rectangle of the screen, in global points
// with the origin at the top-left of the primary screen, leaving out arubox's
// own windows. The caller releases the image.
static int captureRect(CGRect rect, CGImageRef *image) {
	__block SCShareableContent *content = nil;
	__block NSError *failure = nil;
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	[SCShareableContent getShareableContentWithCompletionHandler:^(SCShareableContent *c, NSError *e) {
		content = [c retain];
		failure = [e retain];
		dispatch_semaphore_signal(done);
	}];
	dispatch_semaphore_wait(done, DISPATCH_TIME_FOREVER);
	if (content == nil) {
		int code = [failure code] == SCStreamErrorUserDeclined ? screenDenied : screenFailed;
		[failure release];
		dispatch_release(done);
		return code;
	}
	[failure release];

	CGPoint center = CGPointMake(CGRectGetMidX(rect), CGRectGetMidY(rect));
	SCDisplay *display = nil;
	for (SCDisplay *d in [content displays]) {
		if (CGRectContainsPoint([d frame], center)) {
			display = d;
			break;
		}
	}
	NSMutableArray *own = [NSMutableArray array];
	for (SCRunningApplication *app in [content applications]) {
		if ([app processID] == getpid()) {
			[own addObject:app];
		}
	}
	if (display == nil) {
		[content release];
		dispatch_release(done);
		return screenFailed;
	}

	SCContentFilter *filter = [[SCContentFilter alloc] initWithDisplay:display excludingApplications:own exceptingWindows:@[]];
	SCStreamConfiguration *config = [[SCStreamConfiguration alloc] init];
	CGFloat scale = [filter pointPixelScale];
	CGRect frame = [display frame];
	[config setSourceRect:CGRectMake(rect.origin.x - frame.origin.x, rect.origin.y - frame.origin.y, rect.size.width, rect.size.height)];
	[config setWidth:(size_t)(rect.size.width * scale)];
	[config setHeight:(size_t)(rect.size.height * scale)];
	[config setShowsCursor:NO];

	__block CGImageRef captured = NULL;
	[SCScreenshotManager captureImageWithFilter:filter configuration:config completionHandler:^(CGImageRef i, NSError *e) {
		if (i != NULL) {
			captured = CGImageRetain(i);
		}
		dispatch_semaphore_signal(done);
	}];
	dispatch_semaphore_wait(done, DISPATCH_TIME_FOREVER);
	[config release];
	[filter release];
	[content release];
	dispatch_release(done);
	if (captured == NULL) {
		return screenFailed;
	}
	*image = captured;
	return screenOK;
}

// recognize reads the lines of text in an image. Boxes are in the image's
// pixels with the origin at the top-left.
static int recognize(CGImageRef image, textLine **lines, int *count) {
	VNRecognizeTextRequest *request = [[VNRecognizeTextRequest alloc] init];
	[request setRecognitionLevel:VNRequestTextRecognitionLevelAccurate];
	[request setUsesLanguageCorrection:YES];
	[request setAutomaticallyDetectsLanguage:YES];
	VNImageRequestHandler *handler = [[VNImageRequestHandler alloc] initWithCGImage:image options:@{}];
	BOOL ok = [handler performRequests:@[request] error:nil];
	[handler release];
	if (!ok) {
		[request release];
		return screenFailed;
	}
	NSArray<VNRecognizedTextObservation *> *results = [request results];
	double width = (double)CGImageGetWidth(image);
	double height = (double)CGImageGetHeight(image);
	*lines = calloc([results count] + 1, sizeof(textLine));
	*count = 0;
	for (VNRecognizedTextObservation *observation in results) {
		VNRecognizedText *best = [[observation topCandidates:1] firstObject];
		if (best == nil) {
			continue;
		}
		CGRect box = [observation boundingBox];
		textLine *line = &(*lines)[(*count)++];
		line->text = strdup([[best string] UTF8String]);
		line->x = box.origin.x * width;
		line->y = (1 - box.origin.y - box.size.height) * height;
		line->width = box.size.width * width;
		line->height = box.size.height * height;
	}
	[request release];
	return screenOK;
}

typedef struct {
	int code;
	textLine *lines;
	int count;
} screenResult;

static screenResult screenText(double x, double y, double width, double height) {
	screenResult result = {0};
	@autoreleasepool {
		CGImageRef image = NULL;
		result.code = captureRect(CGRectMake(x, y, width, height), &image);
		if (result.code == screenOK) {
			result.code = recognize(image, &result.lines, &result.count);
			CGImageRelease(image);
		}
	}
	return result;
}

static void freeLines(textLine *lines, int count) {
	for (int i = 0; i < count; i++) {
		free(lines[i].text);
	}
	free(lines);
}
*/
import "C"

import (
	"errors"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ScreenCaptureAllowed reports whether the user granted the screen recording
// permission that reading text off the screen requires.
func ScreenCaptureAllowed() bool {
	return bool(C.screenCaptureAllowed())
}

// RequestScreenCapture shows the system prompt for the screen recording
// permission, unless it is already granted or was refused before.
func RequestScreenCapture() {
	C.requestScreenCapture()
}

// OpenScreenCaptureSettings opens the screen recording page of System
// Settings.
func OpenScreenCaptureSettings() {
	C.openScreenCaptureSettings()
}

// ScreenText reads the text shown inside r, leaving out arubox's own windows.
// Line boxes are in pixels of the captured picture.
func ScreenText(r application.Rect) ([]TextLine, error) {
	res := C.screenText(C.double(r.X), C.double(r.Y), C.double(r.Width), C.double(r.Height))
	switch res.code {
	case C.screenOK:
	case C.screenDenied:
		return nil, ErrNoScreenCapture
	default:
		return nil, errors.New("截取屏幕或识别文字失败")
	}
	defer C.freeLines(res.lines, res.count)
	result := make([]TextLine, 0, int(res.count))
	for _, l := range unsafe.Slice(res.lines, int(res.count)) {
		result = append(result, TextLine{
			Text: C.GoString(l.text),
			Box:  Box{X: float64(l.x), Y: float64(l.y), Width: float64(l.width), Height: float64(l.height)},
		})
	}
	return result, nil
}
