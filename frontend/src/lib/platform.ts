// Which desktop the launcher is running on, for the few places the UI differs:
// the header inset (a frameless macOS window draws its traffic lights over the
// page) and advice that names an OS's own folders.
//
// Read from the user agent rather than the Wails runtime, because it is there
// synchronously before the first paint — WKWebView reports "Macintosh", WebView2
// "Windows NT".

export type OS = "mac" | "windows" | "linux";

export function detectOS(userAgent: string): OS {
  if (/Windows NT/i.test(userAgent)) return "windows";
  if (/Macintosh|Mac OS X/i.test(userAgent)) return "mac";
  return "linux";
}

/** The OS main.ts recorded on <html> at startup. */
export function currentOS(): OS {
  return (document.documentElement.dataset.os as OS | undefined) ?? detectOS(navigator.userAgent);
}
