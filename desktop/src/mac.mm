#import <AppKit/AppKit.h>

// The app lives in the menu bar: no Dock icon, whether it runs inside World Recorder.app (whose Info.plist says so)
// or beside a program installed without the app.
void keepOutOfDock() { [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory]; }
