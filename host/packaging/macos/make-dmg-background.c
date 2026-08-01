// Generates the DMG window background art.
//
// Committed alongside the images it produces so the artwork is reproducible
// rather than a mystery binary. It is not part of the app build; run it only
// when the background needs changing:
//
//   cc -framework CoreGraphics -framework CoreText -framework CoreFoundation \
//      -o /tmp/mkbg make-dmg-background.c && /tmp/mkbg <outdir>
//
// then rebuild the multi-resolution TIFF:
//
//   tiffutil -cathidpicheck <outdir>/dmg-background.png \
//       <outdir>/dmg-background@2x.png -out <outdir>/dmg-background.tiff

#include <CoreFoundation/CoreFoundation.h>
#include <CoreGraphics/CoreGraphics.h>
#include <CoreText/CoreText.h>
#include <ImageIO/ImageIO.h>
#include <stdio.h>
#include <string.h>

// Window content size in points. The build script positions icons against
// exactly these dimensions, so the two must stay in step.
static const int kWidth = 600;
static const int kHeight = 400;

static void drawArrow(CGContextRef ctx, double scale) {
  // A simple chevron pointing from the app icon toward the Applications
  // alias. Muted, so it reads as guidance and not as a button.
  CGContextSetRGBStrokeColor(ctx, 0.55, 0.58, 0.62, 1.0);
  CGContextSetLineWidth(ctx, 6.0 * scale);
  CGContextSetLineCap(ctx, kCGLineCapRound);
  CGContextSetLineJoin(ctx, kCGLineJoinRound);

  // Vertical centre of the icon row. The build script places icons at y=190
  // measured from the top, and this canvas is drawn from the bottom.
  double y = (kHeight - 190) * scale;
  double x0 = 258 * scale;
  double x1 = 342 * scale;

  CGContextBeginPath(ctx);
  CGContextMoveToPoint(ctx, x0, y);
  CGContextAddLineToPoint(ctx, x1, y);
  CGContextStrokePath(ctx);

  CGContextBeginPath(ctx);
  CGContextMoveToPoint(ctx, x1 - 18 * scale, y + 14 * scale);
  CGContextAddLineToPoint(ctx, x1, y);
  CGContextAddLineToPoint(ctx, x1 - 18 * scale, y - 14 * scale);
  CGContextStrokePath(ctx);
}

static void drawText(CGContextRef ctx, double scale, const char *text,
                     double centreY, double fontSize, CGFloat grey) {
  CFStringRef string = CFStringCreateWithCString(NULL, text, kCFStringEncodingUTF8);
  CTFontRef font = CTFontCreateWithName(CFSTR("Helvetica Neue"), fontSize * scale, NULL);
  CGColorRef colour = CGColorCreateGenericRGB(grey, grey, grey, 1.0);

  CFStringRef keys[] = {kCTFontAttributeName, kCTForegroundColorAttributeName};
  CFTypeRef values[] = {font, colour};
  CFDictionaryRef attributes = CFDictionaryCreate(
      NULL, (const void **)keys, (const void **)values, 2,
      &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);

  CFAttributedStringRef attributed = CFAttributedStringCreate(NULL, string, attributes);
  CTLineRef line = CTLineCreateWithAttributedString(attributed);

  CGRect bounds = CTLineGetBoundsWithOptions(line, kCTLineBoundsUseOpticalBounds);
  double x = (kWidth * scale - CGRectGetWidth(bounds)) / 2.0;
  CGContextSetTextPosition(ctx, x, centreY * scale);
  CTLineDraw(line, ctx);

  CFRelease(line);
  CFRelease(attributed);
  CFRelease(attributes);
  CGColorRelease(colour);
  CFRelease(font);
  CFRelease(string);
}

static int render(const char *path, double scale) {
  size_t width = (size_t)(kWidth * scale);
  size_t height = (size_t)(kHeight * scale);

  CGColorSpaceRef space = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
  CGContextRef ctx = CGBitmapContextCreate(NULL, width, height, 8, 0, space,
                                           kCGImageAlphaPremultipliedLast);
  if (!ctx) {
    fprintf(stderr, "failed to create bitmap context\n");
    return 1;
  }

  // Soft vertical gradient. DMG windows open in the Finder's light
  // appearance regardless of system theme, so this is deliberately light.
  CGFloat components[] = {0.973, 0.980, 0.988, 1.0, 0.890, 0.910, 0.933, 1.0};
  CGFloat locations[] = {0.0, 1.0};
  CGGradientRef gradient =
      CGGradientCreateWithColorComponents(space, components, locations, 2);
  CGContextDrawLinearGradient(ctx, gradient, CGPointMake(0, height),
                              CGPointMake(0, 0), 0);
  CGGradientRelease(gradient);

  drawArrow(ctx, scale);
  drawText(ctx, scale, "Drag ESP HID Bridge into Applications", 96, 15.0, 0.35);
  drawText(ctx, scale, "First launch: right-click is not enough on macOS 15+ \xE2\x80\x94 see the README",
           64, 11.0, 0.55);

  CGImageRef image = CGBitmapContextCreateImage(ctx);
  CFStringRef cfPath = CFStringCreateWithCString(NULL, path, kCFStringEncodingUTF8);
  CFURLRef url = CFURLCreateWithFileSystemPath(NULL, cfPath, kCFURLPOSIXPathStyle, false);
  CGImageDestinationRef dest = CGImageDestinationCreateWithURL(url, CFSTR("public.png"), 1, NULL);
  int ok = 0;
  if (dest) {
    CGImageDestinationAddImage(dest, image, NULL);
    ok = CGImageDestinationFinalize(dest) ? 0 : 1;
    CFRelease(dest);
  } else {
    ok = 1;
  }

  CFRelease(url);
  CFRelease(cfPath);
  CGImageRelease(image);
  CGContextRelease(ctx);
  CGColorSpaceRelease(space);
  return ok;
}

int main(int argc, char **argv) {
  const char *outDir = (argc > 1) ? argv[1] : ".";
  char path[1024];

  snprintf(path, sizeof(path), "%s/dmg-background.png", outDir);
  if (render(path, 1.0) != 0) {
    return 1;
  }
  printf("wrote %s\n", path);

  snprintf(path, sizeof(path), "%s/dmg-background@2x.png", outDir);
  if (render(path, 2.0) != 0) {
    return 1;
  }
  printf("wrote %s\n", path);
  return 0;
}
