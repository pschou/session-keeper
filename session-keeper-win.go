package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/getlantern/systray"
	"github.com/getlantern/systray/example/icon"
)

func postSetup() {
	//for k, v := range os.Environ() {
	//	fmt.Printf(" %d=%q\n", k, v)
	//}
	env := os.Environ()
	if len(env) > 1 && !strings.HasPrefix(env[1], "=") {
		getConsoleWindow := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow")
		if getConsoleWindow.Find() != nil {
			return
		}

		showWindow := syscall.NewLazyDLL("user32.dll").NewProc("ShowWindow")
		if showWindow.Find() != nil {
			return
		}

		hwnd, _, _ := getConsoleWindow.Call()
		if hwnd == 0 {
			return
		}

		showWindow.Call(hwnd, 0)
	}
	go systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetTemplateIcon(icon.Data, icon.Data)
	systray.SetIcon(icon.Data)
	systray.SetTitle("SessionKeeper (from " + *listen + " to " + *target + ")")
	systray.SetTooltip("Session keeper (" + *listen + "->" + *target + ")")
	mListen := systray.AddMenuItem("Listen: "+*listen, "Listening on this port for incoming connections")
	mListen.Disable()
	mTarget := systray.AddMenuItem("Target: "+*target, "Target session-server to send any new sessions")
	mTarget.Disable()
	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit", "Quit and close all connections")

	// Sets the icon of a menu item. Only available on Mac and Windows.
	//mQuit.SetIcon(icon.Data)
	go func() {
		for {
			select {
			case <-mQuit.ClickedCh:
				systray.Quit()
				fmt.Println("Quitting now...")
				return
			}
		}
	}()
}

func onExit() {
	os.Exit(0)
}
