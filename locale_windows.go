package main

import "syscall"

func nativeLocale() string {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")
	id, _, _ := proc.Call()
	if id&0x3ff == 0x19 {
		return "ru"
	}
	return "en"
}
