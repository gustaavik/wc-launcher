// Shared launcher state, as runes.
//
// One module rather than a store per concern: everything here is a single
// object's worth of state, and the components that read it all read most of it.

import { Events } from "@wailsio/runtime";
import { AuthService, GameService, UpdateService } from "../../bindings/github.com/gustaavik/wc-launcher/internal/services";
import type { AccountView, GameStatus, LauncherStatus, UpdateStatus } from "../../bindings/github.com/gustaavik/wc-launcher/internal/services";
import type { Progress } from "../../bindings/github.com/gustaavik/wc-launcher/internal/install";

export type Route = "loading" | "login" | "home" | "settings";

/** How many log lines to keep. Enough to see a startup failure, bounded so a
 *  chatty RUST_LOG cannot grow the page without limit. */
const LOG_LIMIT = 400;

class LauncherState {
    route = $state<Route>("loading");
    account = $state<AccountView | null>(null);

    update = $state<UpdateStatus | null>(null);
    progress = $state<Progress | null>(null);
    installing = $state(false);

    /** The launcher's own update. Independent of the game's: it comes from
     *  GitHub rather than the account server, and works signed out. */
    selfUpdate = $state<LauncherStatus | null>(null);
    selfProgress = $state<Progress | null>(null);
    selfBusy = $state(false);
    /** Set when the player closes the update strip, so it stays closed for
     *  this session rather than reappearing on the next check. */
    selfDismissed = $state(false);

    game = $state<GameStatus>({ running: false, pid: 0, exitCode: 0, message: "" });
    log = $state<string[]>([]);

    /** A transient message shown at the top of the home screen. */
    banner = $state("");

    /** True while a Check or Install call is out. */
    busy = $state(false);

    get signedIn(): boolean {
        return this.account !== null;
    }

    /** Whether to show the launcher-update strip at all. */
    get selfUpdateVisible(): boolean {
        const status = this.selfUpdate;
        if (!status || this.selfDismissed) return false;
        return status.updateAvailable && status.supported;
    }

    /** What the big button should say, given everything else. */
    get action(): { label: string; kind: "play" | "install" | "update" | "none"; enabled: boolean } {
        if (this.game.running) return { label: "Running", kind: "none", enabled: false };
        if (this.installing) return { label: "Installing…", kind: "none", enabled: false };

        const status = this.update;
        if (!status) return { label: "Checking…", kind: "none", enabled: false };
        if (!status.supported) return { label: "Unavailable", kind: "none", enabled: false };
        if (!status.installedTag) return { label: "Install", kind: "install", enabled: true };
        if (status.updateAvailable) return { label: "Update", kind: "update", enabled: true };
        if (status.playable) return { label: "Play", kind: "play", enabled: true };
        return { label: "Install", kind: "install", enabled: true };
    }

    /** Wire up the Go events. Called once, from App. */
    listen() {
        Events.On("auth:changed", (event) => {
            // Wails wraps the payload; the value is on .data.
            this.account = event.data ?? null;
            if (!this.account && this.route === "home") this.route = "login";
        });

        Events.On("update:progress", (event) => {
            this.progress = event.data ?? null;
            const phase = this.progress?.phase;
            this.installing = phase === "downloading" || phase === "verifying" || phase === "extracting";
        });

        Events.On("launcher:progress", (event) => {
            this.selfProgress = event.data ?? null;
            const phase = this.selfProgress?.phase;
            this.selfBusy = phase === "downloading" || phase === "verifying" || phase === "extracting";
        });

        Events.On("game:state", (event) => {
            const status = event.data;
            if (!status) return;
            this.game = status;
            if (!status.running && status.message) this.banner = status.message;
        });

        Events.On("game:log", (event) => {
            const line = event.data;
            if (typeof line !== "string") return;
            this.log.push(line);
            if (this.log.length > LOG_LIMIT) this.log.splice(0, this.log.length - LOG_LIMIT);
        });
    }

    /** Restore a stored session, then decide which screen to show. */
    async start() {
        const result = await AuthService.Restore();
        this.account = result.account ?? null;
        // An error here (an outage, an expired token) is shown on the login
        // screen rather than swallowed — the player needs to know which it was.
        if (result.error) this.banner = result.error;
        this.route = this.account ? "home" : "login";
        if (this.account) void this.check();
        // Not gated on being signed in: updating the launcher needs no account,
        // and a launcher too old to sign in is exactly the one that must be
        // able to replace itself.
        void this.checkSelf();
    }

    async check() {
        this.busy = true;
        try {
            this.update = await UpdateService.Check();
        } finally {
            this.busy = false;
        }
    }

    async install() {
        this.installing = true;
        this.banner = "";
        try {
            const error = await UpdateService.Install();
            if (error) this.banner = error;
            await this.check();
        } finally {
            this.installing = false;
            this.progress = null;
        }
    }

    cancelInstall() {
        void UpdateService.Cancel();
    }

    async checkSelf() {
        this.selfUpdate = await UpdateService.CheckLauncher();
    }

    async installSelf() {
        this.selfBusy = true;
        this.banner = "";
        try {
            const error = await UpdateService.InstallLauncher();
            if (error) this.banner = error;
            await this.checkSelf();
        } finally {
            this.selfBusy = false;
            this.selfProgress = null;
        }
    }

    /** Hand over to the downloaded launcher. On success this window goes away:
     *  the new build replaces this one and starts itself. */
    async restartToUpdate() {
        this.banner = "";
        const error = await UpdateService.ApplyLauncherUpdate();
        if (error) this.banner = error;
    }

    cancelSelfInstall() {
        void UpdateService.CancelLauncher();
    }

    async play() {
        this.banner = "";
        this.log = [];
        const error = await GameService.Launch();
        if (error) this.banner = error;
        this.game = await GameService.Status();
    }

    async stopGame() {
        await GameService.Stop();
    }

    async signOut() {
        const result = await AuthService.Logout();
        if (result.error) {
            this.banner = result.error;
            return;
        }
        this.account = null;
        this.update = null;
        this.route = "login";
    }
}

export const launcher = new LauncherState();
