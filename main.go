package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	revelConfig "github.com/revel/config"
	term "github.com/nsf/termbox-go"
	mswfRoutines "github.com/xaionaro-go/mswfAPI/app/common"
	mswfAPIClient "github.com/xaionaro-go/mswfAPI/clientLib"
	curses "github.com/xaionaro-go/goncurses"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	MSWF_API_CONFIG_PATH        = "/root/go/src/github.com/xaionaro-go/mswfAPI/conf/app.conf"
	MSWF_API_CLIENT_CONFIG_PATH = "/etc/mswf-api-client.json"
)

var (
	lineNumRegexp = regexp.MustCompile(`line#\d+`)
	openAtLine    = -1
	window        *curses.Window
	mswfAPI       *mswfAPIClient.MswfAPIClient
	mswfApiConfig *revelConfig.Config
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
	args := []string{"dynamic"}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "editor"
	}
	if openAtLine >= 0 {
		lineNumStr := strconv.Itoa(openAtLine)
		switch editor {
		case "editor", "vi", "vim", "vim.basic":
			args = append([]string{"+" + lineNumStr}, args...)
		case "mcedit":
			args = []string{args[0] + ":" + lineNumStr}
		}
	}
	cmd := exec.Command(editor, args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err := cmd.Run()
	if err != nil {
		panic(err)
	}
}

func checkAndReformatConfig() bool {
	err := mswfRoutines.ReadConfig()
	if err == nil {
		err = mswfRoutines.FWSMConfig.Save(nil, mswfRoutines.FWSM_CONFIG_PATH)
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
		/*if !isConfigChanged() {
			fmt.Println(`Nothing changed.`)
			return
		}*/
		if !checkAndReformatConfig() {
			continue
		}
		/*if !isConfigChanged() {
			fmt.Println(`Nothing changed.`)
			return
		}*/
		err := doReloadConfig()
		if err != nil {
			fmt.Printf("Got error from doReloadConfig(): %v", err.Error())
			continue
		}
		break
	}
}

type mswfAPIClientConfig struct {
	Host   string `defaultValue:"localhost"`
	Port   int    `defaultValue:"9000"`
	User   string `defaultValue:"mswfShell"`
	Pass   string
	Scheme string `defaultValue:"http"`
}

func (cfg mswfAPIClientConfig) Check() error {
	temporaryMswfAPIClient := mswfAPIClient.New(&mswfAPIClient.MswfAPIClientNewArgs{
		Host:   cfg.Host,
		Port:   cfg.Port,
		User:   cfg.User,
		Pass:   cfg.Pass,
		Scheme: cfg.Scheme,
	})

	return temporaryMswfAPIClient.CheckConnection()
}

func tryReinitMswfAPIClientConfigFile() bool {
	defaultValues := map[string]string{}
	fieldNames := []string{}

	reportError := func(msg string, args ...interface{}) {
		curses.DefProgMode()
		curses.End()
		time.Sleep(time.Millisecond*100)
		waitForAnyKey(fmt.Sprintf("Got error \"%v\": %v. Press any key to retry…", msg, args))
		curses.ResetProgMode()
		window.Refresh()
	}

	// scanning structure "mswfAPIClientConfig"

	maxFieldLen := 0
	var mswfAPIClientConfig mswfAPIClientConfig
	mswfAPIClientConfigV := reflect.ValueOf(&mswfAPIClientConfig).Elem()
	for i := 0; i < mswfAPIClientConfigV.NumField(); i++ {
		cfgFieldT := mswfAPIClientConfigV.Type().Field(i)
		cfgFieldName := cfgFieldT.Name
		fieldNames = append(fieldNames, cfgFieldName)
		if len(cfgFieldT.Name) > maxFieldLen {
			maxFieldLen = len(cfgFieldName)
		}
		defaultValues[cfgFieldName] = cfgFieldT.Tag.Get("defaultValue")
	}

	// getting the default password

	userId := 0
	for {
		userLogin, err := mswfApiConfig.String("prod", fmt.Sprintf("user%v.login", userId))
		if userLogin == "" || err != nil {
			break
		}
		if userLogin == defaultValues["User"] {
			defaultValues["Pass"], _ = mswfApiConfig.String("prod", fmt.Sprintf("user%v.password", userId))
		}
		userId++
	}

	// the form

	curses.Echo(false)
	curses.CBreak(true)
	curses.StartColor()
	window.Keypad(true)

	window.Refresh()
	curses.InitPair(1, curses.C_WHITE, curses.C_BLUE)
	curses.InitPair(2, curses.C_YELLOW, curses.C_BLUE)

	fields := map[string]*curses.Field{}
	fieldsArray := []*curses.Field{}
	for idx, fieldName := range fieldNames {
		fields[fieldName], _ = curses.NewField(1, 30, 4+int32(idx), int32(maxFieldLen)+12, 0, 0)
		defer fields[fieldName].Free()
		fields[fieldName].SetForeground(curses.ColorPair(1))
		fields[fieldName].SetBackground(curses.ColorPair(2))
		fields[fieldName].SetOptionsOff(curses.FO_AUTOSKIP)
		fields[fieldName].SetBuffer(defaultValues[fieldName])
		fieldsArray = append(fieldsArray, fields[fieldName])
	}

	form, _ := curses.NewForm(fieldsArray)
	form.Post()
	defer form.UnPost()
	defer form.Free()

	for idx, fieldName := range fieldNames {
		window.MovePrint(4+idx, 10, fieldName+": ")
		form.Driver(curses.REQ_FIRST_FIELD)
	}

	//form.SetCurrentField(fields["Pass"])
	considerActiveField := func() {
		currentField := form.CurrentField()
		for _, fieldName := range fieldNames {
			field := fields[fieldName]
			if field == currentField {
				continue
			}
			field.SetBackground(curses.ColorPair(2))
		}
		currentField.SetBackground(curses.ColorPair(2) | curses.A_UNDERLINE | curses.A_BOLD)
	}

	considerActiveField()

	formIsRunning := true

	window.Refresh()
	for formIsRunning {
		ch := window.GetChar()
		switch ch {
		case curses.KEY_ENTER, 10, 13: // if enter is pressed
			form.Driver(curses.REQ_END_LINE);
			formIsRunning = false
		case curses.KEY_DOWN, curses.KEY_TAB:
			form.Driver(curses.REQ_NEXT_FIELD)
			form.Driver(curses.REQ_END_LINE)
		case curses.KEY_UP:
			form.Driver(curses.REQ_PREV_FIELD)
			form.Driver(curses.REQ_END_LINE)
		case curses.KEY_BACKSPACE:
			form.Driver(curses.REQ_CLR_FIELD)
		default:
			form.Driver(ch)
		}
		considerActiveField()
	}

	// setting resulting values into mswfAPIClientConfig

	for i := 0; i < mswfAPIClientConfigV.NumField(); i++ {
		cfgField  := mswfAPIClientConfigV.Field(i)
		cfgFieldT := mswfAPIClientConfigV.Type().Field(i)
		cfgFieldName := cfgFieldT.Name

		newValue := strings.Trim(fields[cfgFieldName].Buffer(), " ")

		switch cfgField.Interface().(type) {
		case string:
			cfgField.Set(reflect.ValueOf(newValue))
		case int:
			newIntValue, err := strconv.Atoi(newValue)
			if err != nil {
				reportError("invalid integer", newValue, err.Error())
				return false
			}
			cfgField.Set(reflect.ValueOf(newIntValue))
		}
		fieldNames = append(fieldNames, cfgFieldName)
		if len(cfgFieldT.Name) > maxFieldLen {
			maxFieldLen = len(cfgFieldName)
		}
		defaultValues[cfgFieldName] = cfgFieldT.Tag.Get("defaultValue")
	}

	// testing

	{
		err := mswfAPIClientConfig.Check()
		if err != nil {
			reportError("cannot connect to the API server", err.Error())
			return false
		}
	}

	// writting the result into the configuration file

	{
		mswfAPIClientConfigJson, _ := json.MarshalIndent(mswfAPIClientConfig, "", " ")
		err := ioutil.WriteFile(MSWF_API_CLIENT_CONFIG_PATH, mswfAPIClientConfigJson, 0400)
		if err != nil {
			panic(err)
		}
	}

	return true
}

func reinitMswfAPIClientConfigFile() {
	for !tryReinitMswfAPIClientConfigFile() {}
}

func initEverything() {
	var err error

	// MSWF API internal configuration

	fmt.Println("Reading MSWF API internal configuration")
	mswfApiConfig, err = revelConfig.ReadDefault(MSWF_API_CONFIG_PATH)
	if err != nil {
		panic(err)
	}

	// terminal

	fmt.Println("Initializing terminal routines")
	err = term.Init()
	if err != nil {
		panic(err)
	}

	// curses

	fmt.Println("Initializing ncurses routines")
	window, err = curses.Init()
	if err != nil {
		panic(err)
	}

	// chdir()

	dir := filepath.Dir(mswfRoutines.FWSM_CONFIG_PATH)
	os.Chdir(dir)

	// read MSWF API client configuration

	fmt.Println("Initializing MSWF API client")

	for {
		fmt.Println("\tReading MSWF API client configuration")
		mswfAPIClientConfigFile, err := ioutil.ReadFile(MSWF_API_CLIENT_CONFIG_PATH)
		if err != nil {
			reinitMswfAPIClientConfigFile()
			continue
		}
		var mswfAPIClientConfig mswfAPIClientConfig
		json.Unmarshal(mswfAPIClientConfigFile, &mswfAPIClientConfig)

		fmt.Println("\tChecking MSWF API client configuration (connecting to the API server)")
		err = mswfAPIClientConfig.Check()
		if err != nil {
			reinitMswfAPIClientConfigFile()
			continue
		}

		mswfAPI = mswfAPIClient.New(&mswfAPIClient.MswfAPIClientNewArgs{
			Host:   mswfAPIClientConfig.Host,
			Port:   mswfAPIClientConfig.Port,
			User:   mswfAPIClientConfig.User,
			Pass:   mswfAPIClientConfig.Pass,
			Scheme: mswfAPIClientConfig.Scheme,
		})

		break
	}
	fmt.Println("Fully initialized")
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
	fmt.Printf("Success. Applying the new config, please wait...\n")
	err := mswfAPI.Reload()
	if err != nil {
		fmt.Printf("Got error from mswfAPI.Reload(): %v", err.Error())
		return err
	}
	err = mswfAPI.Apply()
	if err != nil {
		fmt.Printf("Got error from mswfAPI.Apply(): %v", err.Error())
		return err
	}
	err = mswfAPI.Save()
	if err != nil {
		fmt.Printf("Got error from mswfAPI.Save(): %v", err.Error())
		return err
	}
	return err
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

	err = doPushConfig()
	if err != nil {
		fmt.Printf("Cannot push changed to the git server: %v", err.Error())
	}

	return nil
}

func runLinuxTerminal() {
	resetScreen()
	err := runCommandInTerminal("screen", "-x", "-S", "mswfShellTerminal")
	if err == nil {
		return
	}
	exec.Command("sh", "-c", `kill $(ls /var/run/screen/*/*.mswfShellTerminal | sed -e 's%.*/%%g' -e 's%\..*%%g') 2>/dev/null`).Run()
	runCommandInTerminal("screen", "-S", "mswfShellTerminal", "/bin/bash")
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
	title := "MSWF"
	menuwin.Box(0, 0)
	menuwin.ColorOn(1)
	menuwin.MovePrint(1, (x/2)-(len(title)/2), title)
	menuwin.ColorOff(1)
	menuwin.MoveAddChar(2, 0, curses.ACS_LTEE)
	menuwin.HLine(2, 1, curses.ACS_HLINE, x-2)
	menuwin.MoveAddChar(2, x-1, curses.ACS_RTEE)

	y, x = window.MaxYX()
	window.MovePrint(y-2, 2, "tech support: mswf@ut.mephi.ru")
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
