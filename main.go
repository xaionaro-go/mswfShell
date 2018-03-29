package main

import (
	"fmt"
	term "github.com/nsf/termbox-go"
	fwsmAPI "github.com/xaionaro-go/fwsmAPI/app/common"
	curses "github.com/xaionaro-go/goncurses"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	lineNumRegexp = regexp.MustCompile(`line#\d+`)
	openAtLine    = -1
	window        *curses.Window
)

func waitForAnyKey(msg string, validKeys ...term.Key) (event term.Event) {
	err := term.Flush()
	if err != nil {
		panic(err)
	}

	if msg != "" {
		fmt.Println(msg)
	}
	for {
		event = term.PollEvent()
		if len(validKeys) == 0 {
			return event
		}

		for _, key := range validKeys {
			if event.Key == key {
				return event
			}
		}
	}

	return term.Event{}
}

func openConfigEditor() {
	args := []string{}
	if openAtLine >= 0 {
		args = []string{"+" + strconv.Itoa(openAtLine), "dynamic"}
	} else {
		args = []string{"dynamic"}
	}
	cmd := exec.Command("editor", args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err := cmd.Run()
	if err != nil {
		panic(err)
	}
}

func checkAndReformatConfig() bool {
	err := fwsmAPI.ReadConfig()
	if err == nil {
		err = fwsmAPI.FWSMConfig.Save(nil, fwsmAPI.FWSM_CONFIG_PATH)
		if err != nil {
			panic(err)
		}

		return true
	}

	lineNumStringValue := lineNumRegexp.FindString(err.Error())
	if lineNumStringValue != "" {
		var err error
		openAtLine, err = strconv.Atoi(strings.Split(lineNumStringValue, "#")[1])
		if err != nil {
			panic(err)
		}
	}
	fmt.Println("\nError:", err.Error())
	event := waitForAnyKey("There're errors. Press \"escape\" to cancel the changes or \"space\" to return.", term.KeyEsc, term.KeySpace)
	switch event.Key {
	case term.KeyEsc:
		exec.Command("git", "stash").Run()
		return true
	}

	return false
}

func clearScreen() {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func resetScreen() {
	cmd := exec.Command("reset")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

func commitConfig() {
	cmd := exec.Command("git", "commit", "dynamic")
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err := cmd.Run()
	if err != nil {
		panic(err)
	}
}

func isConfigChanged() bool {
	cmd := exec.Command("git", "diff", "--exit-code", "dynamic")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err := cmd.Run()
	if err == nil {
		return false
	}
	return true
}

func editRunningConfig() {
	for {
		clearScreen()
		openConfigEditor()
		if !isConfigChanged() {
			fmt.Println(`Nothing changed.`)
			return
		}
		if !checkAndReformatConfig() {
			continue
		}
		if !isConfigChanged() {
			fmt.Println(`Nothing changed.`)
			return
		}
		commitConfig()
		break
	}
}

func initEverything() {
	err := term.Init()
	if err != nil {
		panic(err)
	}

	window, err = curses.Init()
	if err != nil {
		panic(err)
	}

	dir := filepath.Dir(fwsmAPI.FWSM_CONFIG_PATH)

	os.Chdir(dir)
	//initSignals()
}

func deinitEverything() {
	curses.End()
	term.Close()
}

func getTotalTraffic() int {
	stringValue, err := exec.Command("sh", "-c", `ifconfig trunk | awk 'BEGIN {bytes=0} {if ($4=="bytes"){bytes+=$5}} END{print bytes}'`).Output()
	if err != nil {
		panic(err)
	}
	intValue, err := strconv.Atoi(string(stringValue))
	if err != nil {
		panic(err)
	}
	return intValue
}

func showInterfaces() {
	cmd := exec.Command("sh", "-c", "ifconfig | less")
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err := cmd.Run()
	if err != nil {
		panic(err)
	}
}

func commitConfiguration() {
	panic("not implemented, yet")
}

func runLinuxTerminal() {
	resetScreen()
	{
		cmd := exec.Command("screen", "-x", "-S", "openmswfShellTerminal")
		cmd.Stdin = os.Stdin
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		err := cmd.Run()
		if err == nil {
			return
		}
	}
	{
		cmd := exec.Command("screen", "-S", "openmswfShellTerminal", "/bin/bash")
		cmd.Stdin = os.Stdin
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		cmd.Run()
	}
}

func mainWindow() {
	curses.StartColor()
	curses.Raw(true)
	curses.Echo(false)
	curses.Cursor(0)
	window.Keypad(true)
	curses.InitPair(1, curses.C_RED, curses.C_BLACK)

	menu_items := []string{"config terminal", "show interfaces", "copy running-config startup-config", "linux terminal", "exit"}

	items := make([]*curses.MenuItem, len(menu_items))
	for i, val := range menu_items {
		items[i], _ = curses.NewItem(val, "")
		defer items[i].Free()
	}

	// create the menu
	menu, _ := curses.NewMenu(items)
	defer menu.Free()

	menuwin, _ := curses.NewWindow(9, 41, 4, 14)
	menuwin.Keypad(true)

	menu.SetWindow(menuwin)
	dwin := menuwin.Derived(5, 39, 3, 1)
	menu.SubWindow(dwin)
	menu.Mark(" > ")

	// Print centered menu title
	y, x := menuwin.MaxYX()
	title := "OpenMSWF"
	menuwin.Box(0, 0)
	menuwin.ColorOn(1)
	menuwin.MovePrint(1, (x/2)-(len(title)/2), title)
	menuwin.ColorOff(1)
	menuwin.MoveAddChar(2, 0, curses.ACS_LTEE)
	menuwin.HLine(2, 1, curses.ACS_HLINE, x-2)
	menuwin.MoveAddChar(2, x-1, curses.ACS_RTEE)

	y, x = window.MaxYX()
	window.MovePrint(y-2, 2, "tech support: openmswf@ut.mephi.ru")
	window.Refresh()

	menu.Post()
	defer menu.UnPost()
	menuwin.Refresh()

	for {
		curses.Update()
		ch := menuwin.GetChar()

		switch ch {
		case curses.KEY_ENTER, 10, 13: // if enter is pressed
			currentItem := menu.Current(nil)
			currentItemIdx := -1
			for idx, item := range items {
				if *currentItem == *item {
					currentItemIdx = idx
					break
				}
			}
			curses.DefProgMode()
			curses.End()
			switch currentItemIdx {
			case 0:
				editRunningConfig()
			case 1:
				showInterfaces()
			case 2:
				commitConfiguration()
			case 3:
				runLinuxTerminal()
			case 4:
				return
			default:
				panic(fmt.Errorf("Cannot determine menu item index"))
			}
			curses.ResetProgMode()
			window.Refresh()
			menuwin.Refresh()
		case curses.KEY_DOWN:
			menu.Driver(curses.REQ_DOWN)
		case curses.KEY_UP:
			menu.Driver(curses.REQ_UP)
		}
	}

	/*term.SetCursor(3, 0)
	for {

		event := waitForAnyKey("")
		if event.Ch != 0 {
			cmd += string(event.Ch)
			continue
		}

	}
	editRunningConfig()*/
}

func main() {
	initEverything()
	mainWindow()
	//waitForAnyKey(`Press "space" key or "escape" key to exit`, term.KeyEsc, term.KeySpace)
	deinitEverything()
}
