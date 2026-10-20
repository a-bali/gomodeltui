#!/bin/sh
set -eu
cd "$(dirname "$0")"

configuration="${CONFIGURATION:-release}"
scratch_path="${SWIFT_SCRATCH_PATH:-.build}"
version="${VERSION:-$(git -C .. describe --tags --always --dirty 2>/dev/null || printf 'dev')}"
bundle_version="${version#v}"
bundle_version="${bundle_version%%-*}"
case "$bundle_version" in
    *.*.*) ;;
    *) bundle_version="0.0.0" ;;
esac
swift build --scratch-path "$scratch_path" -c "$configuration"
binary_directory="$(swift build --scratch-path "$scratch_path" -c "$configuration" --show-bin-path)"
app="../bin/GoModel.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$binary_directory/GoModelBar" "$app/Contents/MacOS/GoModelBar"
cp Resources/Info.plist "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $bundle_version" "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion $bundle_version" "$app/Contents/Info.plist"
cp ../LICENSE "$app/Contents/Resources/LICENSE"
cp Resources/ThirdPartyNotices.txt "$app/Contents/Resources/ThirdPartyNotices.txt"
# Local ad-hoc signing. Distribution builds should use a Developer ID and notarization.
codesign --force --sign - "$app"
printf 'Built %s\n' "$app"
