//go:build windows

package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	rmwm "github.com/1mm0rt41PC/rmwm-remove-ai-watermarks"
)

const (
	monitorClassName   = "rmwm.clipboard.monitor.v1"
	monitorMutexName   = `Local\rmwm.clipboard.monitor.v1`
	monitorReadyPrefix = `Local\rmwm.clipboard.ready.`
	monitorReadyEnv    = "RMWM_BACKGROUND_READY_EVENT"
	wmClipboardUpdate  = 0x031D
	wmMonitorStop      = 0x8001
	wmClose            = 0x0010
	wmQueryEndSession  = 0x0011
	wmEndSession       = 0x0016
	wmCommand          = 0x0111
	messageYes         = 6
	messageNo          = 7
)

var (
	winRegisterClass       = clipboardUser32.NewProc("RegisterClassW")
	winDefWindowProc       = clipboardUser32.NewProc("DefWindowProcW")
	winAddClipListener     = clipboardUser32.NewProc("AddClipboardFormatListener")
	winRemoveClipListener  = clipboardUser32.NewProc("RemoveClipboardFormatListener")
	winGetMessage          = clipboardUser32.NewProc("GetMessageW")
	winTranslateMessage    = clipboardUser32.NewProc("TranslateMessage")
	winDispatchMessage     = clipboardUser32.NewProc("DispatchMessageW")
	winPostQuitMessage     = clipboardUser32.NewProc("PostQuitMessage")
	winPostMessage         = clipboardUser32.NewProc("PostMessageW")
	winFindWindow          = clipboardUser32.NewProc("FindWindowW")
	winIsWindow            = clipboardUser32.NewProc("IsWindow")
	winSendMessageTimeout  = clipboardUser32.NewProc("SendMessageTimeoutW")
	winMessageBox          = clipboardUser32.NewProc("MessageBoxW")
	winEnumThreadWindows   = clipboardUser32.NewProc("EnumThreadWindows")
	winGetClassName        = clipboardUser32.NewProc("GetClassNameW")
	winGetModuleHandle     = clipboardKernel32.NewProc("GetModuleHandleW")
	winCreateMutex         = clipboardKernel32.NewProc("CreateMutexW")
	winReleaseMutex        = clipboardKernel32.NewProc("ReleaseMutex")
	winCreateEvent         = clipboardKernel32.NewProc("CreateEventW")
	winOpenEvent           = clipboardKernel32.NewProc("OpenEventW")
	winSetEvent            = clipboardKernel32.NewProc("SetEvent")
	winCloseHandle         = clipboardKernel32.NewProc("CloseHandle")
	winOpenProcess         = clipboardKernel32.NewProc("OpenProcess")
	winWaitForMultiple     = clipboardKernel32.NewProc("WaitForMultipleObjects")
	winWaitForSingle       = clipboardKernel32.NewProc("WaitForSingleObject")
	winTerminateProcess    = clipboardKernel32.NewProc("TerminateProcess")
	winGetCurrentThread    = clipboardKernel32.NewProc("GetCurrentThreadId")
	winGetConsoleProcesses = clipboardKernel32.NewProc("GetConsoleProcessList")
	winGetConsoleWindow    = clipboardKernel32.NewProc("GetConsoleWindow")
	winShowWindow          = clipboardUser32.NewProc("ShowWindow")
	winWindowProcess       = clipboardUser32.NewProc("GetWindowThreadProcessId")
	monitorWindows         sync.Map // HWND -> *clipboardMonitor; no Go pointers stored in Win32.
	monitorClassOnce       sync.Once
	monitorClassError      error
	monitorWndProc         = syscall.NewCallback(clipboardMonitorWindowProc)
	monitorCancelProc      = syscall.NewCallback(cancelMonitorDialog)
)

type monitorWindowClass struct {
	Style       uint32
	WndProc     uintptr
	ClassExtra  int32
	WindowExtra int32
	Instance    uintptr
	Icon        uintptr
	Cursor      uintptr
	Background  uintptr
	MenuName    *uint16
	ClassName   *uint16
}

type monitorMessage struct {
	Window  uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   struct{ X, Y int32 }
	Private uint32
}

func findClipboardMonitor() uintptr {
	class, _ := syscall.UTF16PtrFromString(monitorClassName)
	hwnd, _, _ := winFindWindow.Call(uintptr(unsafe.Pointer(class)), 0)
	return hwnd
}

// Hide only a console exclusively attached to this process. In particular, a
// shared cmd/PowerShell/Windows Terminal console must never be hidden. A console
// executable can still flash briefly before this first instruction runs.
func hideOwnConsole() uintptr {
	var pid uint32
	count, _, _ := winGetConsoleProcesses.Call(uintptr(unsafe.Pointer(&pid)), 1)
	if count != 1 || pid != uint32(os.Getpid()) {
		return 0
	}
	hwnd, _, _ := winGetConsoleWindow.Call()
	if hwnd == 0 {
		return 0
	}
	winShowWindow.Call(hwnd, 0) // SW_HIDE; zero means previously hidden, not failure.
	// STARTUPINFO may override the first ShowWindow command. A second call
	// enforces SW_HIDE even when a shortcut requested an initial show state.
	winShowWindow.Call(hwnd, 0)
	return hwnd
}

// The parent waits for this launch's unique ready event or early process exit.
// The worker is created without a console; no registry/service/autostart entry
// is created and the child does not inherit the caller's text streams.
func startClipboardMonitor(cfg config) (resultErr error) {
	hiddenConsole := hideOwnConsole()
	defer func() {
		if resultErr != nil && hiddenConsole != 0 {
			winShowWindow.Call(hiddenConsole, 5) // SW_SHOW: preserve normal stderr diagnostics.
			// A standalone console may disappear when main returns, so also leave
			// an acknowledged native error dialog. Errors contain no copied text.
			showClipboardMessage("La surveillance n'a pas pu démarrer.\r\n\r\n"+resultErr.Error(), 0x10|0x10000)
		}
	}()
	if findClipboardMonitor() != 0 {
		return fmt.Errorf("une surveillance rmwm est déjà active ; utilisez -bg-stop pour la remplacer")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("exécutable rmwm inaccessible : %w", err)
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("initialisation de la surveillance : %w", err)
	}
	readyName := monitorReadyPrefix + hex.EncodeToString(nonce[:])
	wideReady, _ := syscall.UTF16PtrFromString(readyName)
	ready, _, err := winCreateEvent.Call(0, 1, 0, uintptr(unsafe.Pointer(wideReady)))
	if ready == 0 {
		return windowsCallError("CreateEventW (démarrage)", err)
	}
	defer winCloseHandle.Call(ready)
	cmd := exec.Command(exe, monitorWorkerArguments(cfg)...)
	// Environment names are case-insensitive on Windows: replace any inherited
	// handshake variable, rather than letting duplicate names choose a stale event.
	for _, item := range os.Environ() {
		name, _, _ := strings.Cut(item, "=")
		if !strings.EqualFold(name, monitorReadyEnv) {
			cmd.Env = append(cmd.Env, item)
		}
	}
	cmd.Env = append(cmd.Env, monitorReadyEnv+"="+readyName)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000 | 0x00000200, // CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("démarrage du processus de surveillance : %w", err)
	}
	// Hold a separate native process handle while releasing os/exec's handle;
	// the parent never leaves a goroutine waiting for a long-running child.
	process, _, err := winOpenProcess.Call(0x00100000|0x00000001, 0, uintptr(cmd.Process.Pid)) // SYNCHRONIZE | PROCESS_TERMINATE
	if process == 0 {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return windowsCallError("OpenProcess (confirmation du démarrage)", err)
	}
	_ = cmd.Process.Release()
	defer winCloseHandle.Call(process)
	handles := [2]uintptr{ready, process}
	result, _, err := winWaitForMultiple.Call(2, uintptr(unsafe.Pointer(&handles[0])), 0, 10000)
	switch result {
	case 0: // Ready event signaled after window/listener setup.
		return nil
	case 1:
		return fmt.Errorf("la surveillance s'est arrêtée avant son initialisation (session interactive requise ou instance déjà active)")
	case 0x102: // WAIT_TIMEOUT: terminate only the child created by this call.
		winTerminateProcess.Call(process, 1)
		winWaitForSingle.Call(process, 1000)
		return fmt.Errorf("initialisation de la surveillance non confirmée après 10 secondes")
	default:
		winTerminateProcess.Call(process, 1)
		winWaitForSingle.Call(process, 1000)
		return windowsCallError("WaitForMultipleObjects (démarrage)", err)
	}
}

func signalClipboardMonitorReady() error {
	name := os.Getenv(monitorReadyEnv)
	if name == "" {
		return nil
	}
	if !strings.HasPrefix(name, monitorReadyPrefix) {
		return fmt.Errorf("événement de démarrage invalide")
	}
	wideName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return fmt.Errorf("événement de démarrage invalide")
	}
	event, _, err := winOpenEvent.Call(0x0002, 0, uintptr(unsafe.Pointer(wideName))) // EVENT_MODIFY_STATE
	if event == 0 {
		return windowsCallError("OpenEventW (démarrage)", err)
	}
	defer winCloseHandle.Call(event)
	ok, _, err := winSetEvent.Call(event)
	if ok == 0 {
		return windowsCallError("SetEvent (démarrage)", err)
	}
	return nil
}

type clipboardMonitor struct {
	cfg         config
	wake        chan struct{}
	done        chan struct{}
	finished    chan struct{}
	stopOnce    sync.Once
	stopped     atomic.Bool
	popupThread atomic.Uint32
}

func (m *clipboardMonitor) notify() {
	select {
	case m.wake <- struct{}{}:
	default: // Coalesce updates: the processor always reads the latest clipboard.
	}
}

func (m *clipboardMonitor) stop() {
	m.stopOnce.Do(func() {
		m.stopped.Store(true)
		close(m.done)
	})
	m.closePopup()
}

func (m *clipboardMonitor) closePopup() {
	if thread := m.popupThread.Load(); thread != 0 {
		winEnumThreadWindows.Call(uintptr(thread), monitorCancelProc, 0)
	}
}

func cancelMonitorDialog(hwnd, _ uintptr) uintptr {
	var class [32]uint16
	n, _, _ := winGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
	if n != 0 && syscall.UTF16ToString(class[:]) == "#32770" {
		// IDNO cannot approve a pending Yes/No prompt. WM_CLOSE also dismisses
		// an informational OK-only error box on this dedicated dialog thread.
		winPostMessage.Call(hwnd, wmCommand, messageNo, 0)
		winPostMessage.Call(hwnd, wmClose, 0, 0)
	}
	return 1
}

func clipboardMonitorWindowProc(hwnd, message, wparam, lparam uintptr) uintptr {
	if value, ok := monitorWindows.Load(hwnd); ok {
		monitor := value.(*clipboardMonitor)
		switch message {
		case wmClipboardUpdate:
			monitor.notify()
			return 0
		case wmMonitorStop, wmClose:
			monitor.stop()
			winPostQuitMessage.Call(0)
			return 1
		case wmQueryEndSession:
			return 1
		case wmEndSession:
			if wparam != 0 {
				monitor.stop()
				winPostQuitMessage.Call(0)
			}
			return 0
		}
	}
	result, _, _ := winDefWindowProc.Call(hwnd, message, wparam, lparam)
	return result
}

func registerClipboardMonitorWindow() error {
	monitorClassOnce.Do(func() {
		instance, _, err := winGetModuleHandle.Call(0)
		if instance == 0 {
			monitorClassError = windowsCallError("GetModuleHandleW", err)
			return
		}
		className, _ := syscall.UTF16PtrFromString(monitorClassName)
		class := monitorWindowClass{WndProc: monitorWndProc, Instance: instance, ClassName: className}
		atom, _, err := winRegisterClass.Call(uintptr(unsafe.Pointer(&class)))
		if atom == 0 {
			monitorClassError = windowsCallError("RegisterClassW", err)
		}
	})
	return monitorClassError
}

func runClipboardMonitor(cfg config) error {
	// Normally CREATE_NO_WINDOW already guarantees this. Also cover direct
	// execution of the internal worker switch without hiding a shared terminal.
	hideOwnConsole()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	wideMutex, _ := syscall.UTF16PtrFromString(monitorMutexName)
	mutex, _, err := winCreateMutex.Call(0, 1, uintptr(unsafe.Pointer(wideMutex)))
	if mutex == 0 {
		return windowsCallError("CreateMutexW (surveillance)", err)
	}
	defer winCloseHandle.Call(mutex)
	if err == syscall.ERROR_ALREADY_EXISTS {
		return fmt.Errorf("une surveillance rmwm est déjà active dans cette session")
	}
	defer winReleaseMutex.Call(mutex)
	if err := registerClipboardMonitorWindow(); err != nil {
		return err
	}
	class, _ := syscall.UTF16PtrFromString(monitorClassName)
	name, _ := syscall.UTF16PtrFromString("rmwm Clipboard Monitor")
	instance, _, _ := winGetModuleHandle.Call(0)
	// A hidden top-level tool window can be discovered for -bg-stop. It is
	// never shown and never creates a taskbar button; no tray framework needed.
	hwnd, _, err := winCreateWindowEx.Call(0x80, uintptr(unsafe.Pointer(class)),
		uintptr(unsafe.Pointer(name)), 0, 0, 0, 0, 0, 0, 0, instance, 0)
	if hwnd == 0 {
		return windowsCallError("CreateWindowExW (surveillance)", err)
	}
	defer winDestroyWindow.Call(hwnd)
	m := &clipboardMonitor{cfg: cfg, wake: make(chan struct{}, 1), done: make(chan struct{}), finished: make(chan struct{})}
	monitorWindows.Store(hwnd, m)
	defer monitorWindows.Delete(hwnd)
	ok, _, err := winAddClipListener.Call(hwnd)
	if ok == 0 {
		return windowsCallError("AddClipboardFormatListener", err)
	}
	defer winRemoveClipListener.Call(hwnd)
	go m.go_process()
	defer m.shutdownProcessor()
	if err := signalClipboardMonitorReady(); err != nil {
		return err
	}
	m.notify() // Inspect the current text once, then only clipboard updates.
	var msg monitorMessage
	for {
		result, _, err := winGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) == -1 {
			return windowsCallError("GetMessageW", err)
		}
		if result == 0 {
			return nil
		}
		winTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		winDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (m *clipboardMonitor) shutdownProcessor() {
	m.stop()
	// A stop can race with MessageBox creation. Retry cancellation while the
	// worker finishes, keeping the monitor HWND alive until shutdown completes.
	// Never terminate the process during EmptyClipboard/SetClipboardData or
	// restoration. A blocked clipboard owner can delay this graceful exit;
	// -bg-stop reports its own bounded timeout without killing the worker.
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.finished:
			return
		case <-ticker.C:
			m.closePopup()
		}
	}
}

func clipboardPromptText(stats rmwm.CleanStats) string {
	return fmt.Sprintf("Des caractères Unicode à nettoyer ont été détectés dans le presse-papiers.\r\n\r\n"+
		"Caractères à supprimer : %d\r\nCaractères à remplacer : %d\r\n\r\n"+
		"Nettoyer ce texte ?\r\n\r\n"+
		"Le contenu sera remplacé par du texte brut (mise en forme supprimée).\r\n"+
		"Si une nouvelle copie intervient, elle sera conservée.", stats.Removed, stats.Replaced)
}

func showClipboardMessage(text string, flags uintptr) uintptr {
	body, _ := syscall.UTF16PtrFromString(text)
	title, _ := syscall.UTF16PtrFromString("rmwm — Presse-papiers")
	answer, _, _ := winMessageBox.Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), flags)
	return answer
}

func (m *clipboardMonitor) go_process() {
	debug.SetPanicOnFault(true)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(m.finished)
	thread, _, _ := winGetCurrentThread.Call()
	m.popupThread.Store(uint32(thread))
	defer m.popupThread.Store(0)
	// Remember only the last consecutive declined content, as a hash. A
	// different observed text or non-text copy resets it; no history is retained.
	var declinedDigest [sha256.Size]byte
	haveDeclinedDigest := false
	var ownDigest [sha256.Size]byte
	haveOwnDigest := false
	for {
		select {
		case <-m.done:
			return
		case <-m.wake:
		}
		if m.stopped.Load() {
			return
		}
		previous, err := readClipboard(m.cfg.maxBytes)
		if err != nil || m.stopped.Load() {
			if errors.Is(err, errClipboardNoText) {
				haveDeclinedDigest, haveOwnDigest = false, false
			}
			continue // Non-text, file copies, invalid text, or a busy clipboard.
		}
		digest := sha256.Sum256([]byte(previous.text))
		if haveDeclinedDigest && digest != declinedDigest {
			haveDeclinedDigest = false
		}
		if haveOwnDigest && digest != ownDigest {
			haveOwnDigest = false
		}
		if haveOwnDigest && digest == ownDigest || haveDeclinedDigest && digest == declinedDigest {
			continue
		}
		cleaned, stats, err := cleanClipboardText(m.cfg, previous.text)
		if err != nil || cleaned == previous.text || m.stopped.Load() {
			continue
		}
		sequence, _, _ := winClipboardSequence.Call()
		if uint32(sequence) != previous.sequence {
			m.notify()
			continue
		}
		// ReadClipboard has already CLOSED the clipboard. The native dialog
		// runs on this worker thread, never on the clipboard listener's loop.
		answer := showClipboardMessage(clipboardPromptText(stats), 0x4|0x100|0x40|0x10000|0x40000) // YESNO, default NO, information, foreground/topmost
		if m.stopped.Load() {
			return // In particular, a late Yes after -bg-stop cannot start a write.
		}
		if answer != messageYes {
			declinedDigest, haveDeclinedDigest = digest, true
			continue
		}
		err = replaceWindowsClipboardWithGuard(previous, cleaned, m.cfg.maxBytes, m.stopped.Load)
		if err == nil {
			ownDigest, haveOwnDigest = sha256.Sum256([]byte(cleaned)), true
		} else if errors.Is(err, errClipboardChanged) {
			m.notify() // Re-read the newest copy; never write the stale proposal.
		} else if !m.stopped.Load() {
			showClipboardMessage("Le nettoyage n'a pas pu être appliqué au presse-papiers.\r\n"+
				"Si nécessaire, recopiez le texte depuis son application d'origine.", 0x10|0x10000) // OK + error; no copied content in diagnostics.
		}
	}
}

func stopClipboardMonitor() error {
	hwnd := findClipboardMonitor()
	if hwnd == 0 {
		return nil // Stopping an absent monitor is intentionally idempotent.
	}
	var pid uint32
	winWindowProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return nil // It exited between FindWindow and GetWindowThreadProcessId.
	}
	process, _, err := winOpenProcess.Call(0x00100000, 0, uintptr(pid)) // SYNCHRONIZE, no termination rights.
	if process == 0 {
		if exists, _, _ := winIsWindow.Call(hwnd); exists == 0 {
			return nil
		}
		return windowsCallError("OpenProcess (confirmation de l'arrêt)", err)
	}
	defer winCloseHandle.Call(process)
	if exited, _, _ := winWaitForSingle.Call(process, 0); exited == 0 {
		return nil
	}
	var reply uintptr
	ok, _, err := winSendMessageTimeout.Call(hwnd, wmMonitorStop, 0, 0,
		0x0002|0x0001, 1000, uintptr(unsafe.Pointer(&reply))) // ABORTIFHUNG | BLOCK
	if ok == 0 {
		if exited, _, _ := winWaitForSingle.Call(process, 0); exited == 0 {
			return nil
		}
		return windowsCallError("demande d'arrêt de la surveillance", err)
	}
	// Window destruction precedes final mutex release. Wait for process exit
	// so an immediate restart cannot race the previous singleton's cleanup.
	result, _, err := winWaitForSingle.Call(process, 4000)
	switch result {
	case 0:
		return nil
	case 0x102:
		return fmt.Errorf("arrêt demandé, mais sa confirmation n'a pas été reçue")
	default:
		return windowsCallError("WaitForSingleObject (arrêt)", err)
	}
}
