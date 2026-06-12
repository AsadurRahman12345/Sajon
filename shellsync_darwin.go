// shellsync_darwin.go — macOS Shell Integration (No-Op / Future Placeholder)
//
// Build tag: compiled ONLY on macOS (GOOS=darwin).
//
// WHY THIS IS A NO-OP:
// macOS file-type icon associations require a registered App Bundle
// (.app/Info.plist with CFBundleDocumentTypes and UTExportedTypeDeclarations).
// Without a proper .app bundle, `lsregister` and `LaunchServices` cannot
// associate a bare binary with a custom document icon.
//
// FUTURE PLAN (when a macOS installer is built):
//  1. Create Sajon.app/Contents/Info.plist with CFBundleDocumentTypes
//  2. Embed the icon in Sajon.app/Contents/Resources/sajon.icns
//  3. Run `/System/Library/Frameworks/CoreServices.framework/Versions/A/Frameworks/
//     LaunchServices.framework/Versions/A/Support/lsregister -f Sajon.app`
//
// Until then, this file guarantees that:
//  - The compiler NEVER panics or crashes on macOS
//  - No stub errors pollute the terminal
//  - The build compiles cleanly on darwin

package main

// runShellSync is a deliberate no-op on macOS.
// macOS shell integration requires an App Bundle (.app) with Info.plist —
// planned for a future macOS installer release.
// Returns false always (no first-time message shown).
func runShellSync() bool {
	return false // intentional no-op: see file header for future roadmap
}

// regKeyExists stub — not applicable on macOS.
func regKeyExists(_ string) bool { return false }
