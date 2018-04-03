package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	term "github.com/nsf/termbox-go"
	fwsmRoutines "github.com/xaionaro-go/fwsmAPI/app/common"
	fwsmAPIClient "github.com/xaionaro-go/fwsmAPI/clientLib"
	curses "github.com/xaionaro-go/goncurses"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	FWSM_API_CLIENT_CONFIG_PATH = "/etc/fwsm-api-client.json"
)

var (
	lineNumRegexp = regexp.MustCompile(`line#\d+`)
	openAtLine    = -1
	window        *curses.Window
	fwsmAPI       *fwsmAPIClient.FwsmAPIClient
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
	err := fwsmRoutines.ReadConfig()
	if err == nil {
		err = fwsmRoutines.FWSMConfig.Save(nil, fwsmRoutines.FWSM_CONFIG_PATH)
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
		doStashConfig()
		openAtLine = -1
		return true
	}

	return false
}

func clearScreen() {
	runCommandInTerminal("clear")
}

func resetScreen() {
	runCommandInTerminal("reset")
}

func runCommandInTerminal(cmdStrings ...string) error {
	cmd := exec.Command(cmdStrings[0], cmdStrings[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func executeCommand(cmdStrings ...string) error {
	cmd := exec.Command(cmdStrings[0], cmdStrings[1:]...)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("Got an error while execution of %v: %v\nstdout: %v\nstderr: %v", cmdStrings, err, out.String(), stderr.String())
	}
	return nil
}

func doStashConfig() error {
	return executeCommand("git", "stash")
}

func doCommitConfig() error {
	return runCommandInTerminal("git", "commit", "dynamic")
}

func doPushConfig() error {
	return runCommandInTerminal("git", "push")
}

func isConfigChanged() bool {
	err := runCommandInTerminal("git", "diff", "--exit-code", "dynamic")
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
		doReloadConfig()
		break
	}
}

type fwsmAPIClientConfig struct {
	Host   string
	Port   int
	User   string
	Pass   string
	Scheme string
}

func reinitFwsmAPIClientConfigFile() {

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

	dir := filepath.Dir(fwsmRoutines.FWSM_CONFIG_PATH)

	os.Chdir(dir)
	//initSignals()

	fwsmAPIClientConfigFile, err := ioutil.ReadFile(FWSM_API_CLIENT_CONFIG_PATH)
	if err != nil {
		reinitFwsmAPIClientConfigFile()
	}
	var fwsmAPIClientConfig fwsmAPIClientConfig
	json.Unmarshal(fwsmAPIClientConfigFile, &fwsmAPIClientConfig)

	fwsmAPI = fwsmAPIClient.New(&fwsmAPIClient.FwsmAPIClientNewArgs{
		Host:   fwsmAPIClientConfig.Host,
		Port:   fwsmAPIClientConfig.Port,
		User:   fwsmAPIClientConfig.User,
		Pass:   fwsmAPIClientConfig.Pass,
		Scheme: fwsmAPIClientConfig.Scheme,
	})
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
	err := runCommandInTerminal("sh", "-c", "clear; ifconfig | less")
	if err != nil {
		panic(err)
	}
}

func showARP() {
	err := runCommandInTerminal("sh", "-c", "clear; arp -na | sort | less")
	if err != nil {
		panic(err)
	}
}

func doReloadConfig() error {
	err := fwsmAPI.Reload()
	if err != nil {
		return err
	}
	return fwsmAPI.Apply()
}

func stashConfiguration() error {
	err := doStashConfig()
	if err != nil {
		return err
	}

	return doReloadConfig()
}

func commitConfiguration() error {
	err := doCommitConfig()
	if err != nil {
		return err
	}
	return doPushConfig()
}

func runLinuxTerminal() {
	resetScreen()
	err := runCommandInTerminal("screen", "-x", "-S", "openmswfShellTerminal")
	if err == nil {
		return
	}
	exec.Command("sh", "-c", `kill $(ls /var/run/screen/*/*.openmswfShellTerminal | sed -e 's%.*/%%g' -e 's%\..*%%g') 2>/dev/null`).Run()
	runCommandInTerminal("screen", "-S", "openmswfShellTerminal", "/bin/bash")
}

func mainWindow() {
	curses.StartColor()
	curses.Raw(true)
	curses.Echo(false)
	curses.Cursor(0)
	window.Keypad(true)
	curses.InitPair(1, curses.C_RED, curses.C_BLACK)

	menu_items := []string{"config terminal", "show interfaces", "show arp", "copy running-config startup-config", "copy startup-config running-config", "linux terminal", "exit"}

	items := make([]*curses.MenuItem, len(menu_items))
	for i, val := range menu_items {
		items[i], _ = curses.NewItem(val, "")
		defer items[i].Free()
	}

	// create the menu
	menu, _ := curses.NewMenu(items)
	defer menu.Free()

	menuwin, _ := curses.NewWindow(len(menu_items)+4, 41, 4, 14)
	menuwin.Keypad(true)

	menu.SetWindow(menuwin)
	dwin := menuwin.Derived(len(menu_items), 39, 3, 1)
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

	textOutput, _ := curses.NewWindow(24, 80, 6, 58)

	printError := func(err error) {
		if err == nil {
			err = fmt.Errorf("unknown error")
		}
		textOutput.MovePrint(0, 0, err.Error())
		textOutput.Refresh()
		return
	}
	printSuccess := func() {
		textOutput.MovePrint(0, 0, "OK!")
		textOutput.Refresh()
		return
	}
	clearOutput := func() {
		textOutput.Clear()
		textOutput.Refresh()
	}

	for {
		curses.Update()
		ch := menuwin.GetChar()
		clearOutput()

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
				printSuccess()
			case 1:
				showInterfaces()
				printSuccess()
			case 2:
				showARP()
				printSuccess()
			case 3:
				err := commitConfiguration()
				if err != nil {
					printError(err)
				} else {
					printSuccess()
				}
			case 4:
				err := stashConfiguration()
				if err != nil {
					printError(err)
				} else {
					printSuccess()
				}
			case 5:
				runLinuxTerminal()
				printSuccess()
			case 6:
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
