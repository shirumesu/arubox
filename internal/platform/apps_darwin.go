package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework CoreServices
#import <Cocoa/Cocoa.h>
#import <CoreServices/CoreServices.h>
#include <stdlib.h>

// appendApps lists the bundles in dir, one level into plain folders, as
// "path\tname\talias\n" lines. name is the localized name Finder shows;
// alias is the bundle's own name, often the English one.
static void appendApps(NSMutableString *out, NSString *dir, int depth) {
	NSFileManager *fm = [NSFileManager defaultManager];
	for (NSString *entry in [fm contentsOfDirectoryAtPath:dir error:nil]) {
		if ([entry hasPrefix:@"."]) {
			continue;
		}
		NSString *path = [dir stringByAppendingPathComponent:entry];
		if ([[entry pathExtension] isEqualToString:@"app"]) {
			NSString *name = [[fm displayNameAtPath:path] stringByDeletingPathExtension];
			NSString *alias = [[NSBundle bundleWithPath:path] objectForInfoDictionaryKey:@"CFBundleName"];
			if (![alias isKindOfClass:[NSString class]] || [alias isEqualToString:name]) {
				alias = @"";
			}
			[out appendFormat:@"%@\t%@\t%@\n", path, name, alias];
			continue;
		}
		BOOL isDir = NO;
		if (depth > 0 && [fm fileExistsAtPath:path isDirectory:&isDir] && isDir) {
			appendApps(out, path, depth - 1);
		}
	}
}

static char *applications(void) {
	@autoreleasepool {
		NSMutableString *out = [NSMutableString string];
		NSArray *dirs = @[
			@"/Applications",
			@"/System/Applications",
			[NSHomeDirectory() stringByAppendingPathComponent:@"Applications"],
		];
		for (NSString *dir in dirs) {
			appendApps(out, dir, 1);
		}
		// Finder lives among the system's helpers, which are not listed.
		NSString *finder = @"/System/Library/CoreServices/Finder.app";
		[out appendFormat:@"%@\t%@\tFinder\n", finder,
			[[[NSFileManager defaultManager] displayNameAtPath:finder] stringByDeletingPathExtension]];
		return strdup([out UTF8String]);
	}
}

// iconPNG draws the icon Finder shows for path at size points, at twice the
// pixels for Retina screens.
static void *iconPNG(const char *path, int size, int *length) {
	@autoreleasepool {
		NSImage *icon = [[NSWorkspace sharedWorkspace] iconForFile:[NSString stringWithUTF8String:path]];
		NSInteger pixels = size * 2;
		NSBitmapImageRep *rep = [[NSBitmapImageRep alloc]
			initWithBitmapDataPlanes:NULL pixelsWide:pixels pixelsHigh:pixels bitsPerSample:8 samplesPerPixel:4
			hasAlpha:YES isPlanar:NO colorSpaceName:NSCalibratedRGBColorSpace bytesPerRow:0 bitsPerPixel:0];
		[NSGraphicsContext saveGraphicsState];
		[NSGraphicsContext setCurrentContext:[NSGraphicsContext graphicsContextWithBitmapImageRep:rep]];
		[icon drawInRect:NSMakeRect(0, 0, pixels, pixels) fromRect:NSZeroRect operation:NSCompositingOperationCopy fraction:1.0];
		[NSGraphicsContext restoreGraphicsState];
		NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
		[rep release];
		*length = (int)[png length];
		void *bytes = malloc([png length]);
		memcpy(bytes, [png bytes], [png length]);
		return bytes;
	}
}

static bool openPath(const char *path) {
	@autoreleasepool {
		NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
		return [[NSWorkspace sharedWorkspace] openURL:url];
	}
}

static void revealPath(const char *path) {
	@autoreleasepool {
		NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
		[[NSWorkspace sharedWorkspace] activateFileViewerSelectingURLs:@[url]];
	}
}

// searchFiles asks Spotlight for files and folders in the home folder whose
// name contains text, as "path\tlastUsed\n" lines, lastUsed being when the
// user last opened it in seconds since 1970, or 0.
static char *searchFiles(const char *text, int limit) {
	@autoreleasepool {
		NSString *needle = [NSString stringWithUTF8String:text];
		// Spotlight's query language quotes with " and treats * as a wildcard.
		needle = [needle stringByReplacingOccurrencesOfString:@"\\" withString:@"\\\\"];
		needle = [needle stringByReplacingOccurrencesOfString:@"\"" withString:@"\\\""];
		needle = [needle stringByReplacingOccurrencesOfString:@"*" withString:@"\\*"];
		NSString *q = [NSString stringWithFormat:@"kMDItemFSName == \"*%@*\"cd && kMDItemContentType != \"com.apple.application-bundle\"", needle];
		MDQueryRef query = MDQueryCreate(kCFAllocatorDefault, (CFStringRef)q, NULL, NULL);
		if (query == NULL) {
			return NULL;
		}
		MDQuerySetSearchScope(query, (CFArrayRef)@[(id)kMDQueryScopeHome], 0);
		MDQuerySetMaxCount(query, limit);
		NSMutableString *out = [NSMutableString string];
		if (MDQueryExecute(query, kMDQuerySynchronous)) {
			CFIndex count = MDQueryGetResultCount(query);
			for (CFIndex i = 0; i < count; i++) {
				MDItemRef item = (MDItemRef)MDQueryGetResultAtIndex(query, i);
				CFStringRef path = MDItemCopyAttribute(item, kMDItemPath);
				if (path == NULL) {
					continue;
				}
				CFDateRef used = MDItemCopyAttribute(item, kMDItemLastUsedDate);
				long seconds = used == NULL ? 0 : (long)[(NSDate *)used timeIntervalSince1970];
				[out appendFormat:@"%@\t%ld\n", (NSString *)path, seconds];
				CFRelease(path);
				if (used != NULL) {
					CFRelease(used);
				}
			}
		}
		CFRelease(query);
		return strdup([out UTF8String]);
	}
}
*/
import "C"

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

// Applications lists the installed applications. On macOS an app's ID is its
// bundle path.
func Applications() []App {
	text := C.applications()
	defer C.free(unsafe.Pointer(text))
	var apps []App
	for line := range strings.Lines(C.GoString(text)) {
		fields := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
		if len(fields) != 3 || fields[1] == "" {
			continue
		}
		apps = append(apps, App{ID: fields[0], Name: fields[1], Alias: fields[2]})
	}
	return apps
}

// Icon returns the icon of an app (by ID) or a file, as a PNG of the given
// size in points.
func Icon(id string, size int) []byte {
	path := C.CString(id)
	defer C.free(unsafe.Pointer(path))
	var length C.int
	bytes := C.iconPNG(path, C.int(size), &length)
	defer C.free(bytes)
	return C.GoBytes(bytes, length)
}

// OpenApp starts an app, or brings it to the front.
func OpenApp(id string) error {
	return OpenFile(id)
}

// OpenFile opens a file or folder with its default program.
func OpenFile(path string) error {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	if !C.openPath(p) {
		return errors.New("无法打开 " + path)
	}
	return nil
}

// RevealFile shows a file in Finder.
func RevealFile(path string) {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	C.revealPath(p)
}

// SearchFiles returns files and folders in the home folder whose name
// contains text, from Spotlight's index. It asks for up to limit and leaves
// out what a person rarely means: hidden folders, the Library folder,
// dependency folders and the insides of packages such as apps.
func SearchFiles(text string, limit int) []FoundFile {
	t := C.CString(text)
	defer C.free(unsafe.Pointer(t))
	out := C.searchFiles(t, C.int(limit))
	if out == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(out))
	var files []FoundFile
	for line := range strings.Lines(C.GoString(out)) {
		path, used, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "\t")
		if path == "" || buried(path) {
			continue
		}
		seconds, _ := strconv.ParseInt(used, 10, 64)
		f := FoundFile{Path: path}
		if seconds > 0 {
			f.LastUsed = time.Unix(seconds, 0)
		}
		files = append(files, f)
	}
	return files
}

// packages are folders macOS shows as single files.
var packages = []string{".app", ".bundle", ".framework", ".photoslibrary", ".xcodeproj", ".pkg", ".plugin", ".kext"}

// buried reports whether a path lies where a person rarely looks: hidden
// folders, ~/Library, dependency folders and the insides of packages.
func buried(path string) bool {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts[:len(parts)-1] {
		if strings.HasPrefix(part, ".") || part == "node_modules" || part == "site-packages" ||
			(i == 2 && part == "Library") || (part == "pkg" && i+1 < len(parts) && parts[i+1] == "mod") {
			return true
		}
		for _, ext := range packages {
			if strings.HasSuffix(part, ext) {
				return true
			}
		}
	}
	return false
}
