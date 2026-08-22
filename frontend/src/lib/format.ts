// Formatting shared by the game update and the launcher's own.

/** The download phases the Go side reports, in words. */
export function phaseLabel(phase: string): string {
    switch (phase) {
        case "downloading": return "Downloading";
        case "verifying": return "Verifying";
        case "extracting": return "Unpacking";
        case "done": return "Done";
        case "cancelled": return "Cancelled";
        case "failed": return "Failed";
        default: return phase;
    }
}

export function mb(bytes: number): string {
    return (bytes / 1024 / 1024).toFixed(1);
}

export function shortDate(iso: string): string {
    if (!iso) return "";
    const at = new Date(iso);
    return Number.isNaN(at.getTime())
        ? ""
        : at.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}
