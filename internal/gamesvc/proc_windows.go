package gamesvc

import "syscall"

// createNoWindow is CREATE_NO_WINDOW: the child gets no console of its own.
const createNoWindow = 0x08000000

// sysProcAttr keeps a console window from flashing up beside the game.
//
// A release build of the game is a GUI-subsystem program and opens no console
// anyway, but a developer build pointed at by WCL_DEV_GAME_DIR is not. Its
// stderr still reaches the launcher, through the pipe rather than a window.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
