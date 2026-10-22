//go:build windows && cgo

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include "../native/native.h"
#include <shellapi.h>
#include <psapi.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static HHOOK keyboard_hook;
static UINT html_format, rtf_format;

const char *pt_probe(void) {
    HDESK desktop = OpenInputDesktop(0, FALSE, DESKTOP_READOBJECTS);
    if (!desktop)
        return "unavailable|an interactive input desktop is required; session zero and secure "
               "desktops are not supported";
    CloseDesktop(desktop);
    return "supported|Ctrl+V, Ctrl+Shift+V and Shift+Insert in this interactive desktop; remapped "
           "shortcuts and secure or elevated destinations may be unobservable";
}

static char *utf8(const wchar_t *text, int len) {
    int bytes = WideCharToMultiByte(CP_UTF8, 0, text, len, NULL, 0, NULL, NULL);
    if (bytes <= 0)
        return NULL;
    char *out = (char *)malloc((size_t)bytes + 1);
    if (!out)
        return NULL;
    WideCharToMultiByte(CP_UTF8, 0, text, len, out, bytes, NULL, NULL);
    out[bytes] = 0;
    return out;
}

static void window_context(pt_json *j) {
    HWND window = GetForegroundWindow();
    if (!window)
        return;
    DWORD pid = 0;
    GetWindowThreadProcessId(window, &pid);
    pt_put(j, ",\"pid\":");
    pt_number(j, pid);
    char id[32];
    snprintf(id, sizeof(id), "%llu", (unsigned long long)(uintptr_t)window);
    pt_property(j, "window", id);
    RECT bounds;
    if (GetWindowRect(window, &bounds))
        pt_bounds(j, "bounds", bounds.left, bounds.top, bounds.right - bounds.left,
                  bounds.bottom - bounds.top);
    POINT origin = {0, 0};
    if (GetClientRect(window, &bounds) && ClientToScreen(window, &origin))
        pt_bounds(j, "content_bounds", origin.x, origin.y, bounds.right - bounds.left,
                  bounds.bottom - bounds.top);
    wchar_t title[4096];
    int chars = GetWindowTextW(window, title, 4096);
    if (chars > 0) {
        char *value = utf8(title, chars);
        if (value) {
            pt_property(j, "title", value);
            free(value);
        }
    }
    HANDLE process = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid);
    if (process) {
        wchar_t path[32768];
        DWORD length = 32768;
        if (QueryFullProcessImageNameW(process, 0, path, &length)) {
            char *value = utf8(path, (int)length);
            if (value) {
                pt_property(j, "executable", value);
                char *name = strrchr(value, '\\');
                pt_property(j, "app", name ? name + 1 : value);
                free(value);
            }
        }
        CloseHandle(process);
    }
    process = OpenProcess(PROCESS_QUERY_INFORMATION | PROCESS_VM_READ, FALSE, pid);
    if (process) {
        PROCESS_MEMORY_COUNTERS info;
        if (GetProcessMemoryInfo(process, &info, sizeof(info))) {
            pt_put(j, ",\"memory_usage\":");
            pt_number(j, info.WorkingSetSize);
        }
        CloseHandle(process);
    }
}

static void clipboard_format(pt_json *j, UINT format, const char *media, int *first) {
    HGLOBAL object = (HGLOBAL)GetClipboardData(format);
    if (!object)
        return;
    SIZE_T size = GlobalSize(object);
    void *data = GlobalLock(object);
    if (!data)
        return;
    if (format == CF_UNICODETEXT) {
        size_t units = size / sizeof(wchar_t), length = 0;
        const wchar_t *text = (const wchar_t *)data;
        while (length < units && text[length])
            length++;
        /* Bound conversion work while retaining an explicit truncation signal. */
        size_t kept = length > (size_t)pt_limit ? (size_t)pt_limit : length;
        if (kept && kept < length && text[kept - 1] >= 0xD800 && text[kept - 1] <= 0xDBFF)
            kept--;
        char *value = utf8(text, (int)kept);
        if (value) {
            pt_format(j, media, value, strlen(value), first);
            free(value);
            if (kept < length)
                snprintf(pt_failure, sizeof(pt_failure),
                         "clipboard text exceeds native conversion limit");
        } else if (length == 0)
            pt_format(j, media, "", 0, first);
    } else {
        size_t n = 0;
        while (n < size && ((char *)data)[n])
            n++;
        pt_format(j, media, data, n, first);
    }
    GlobalUnlock(object);
}

static void capture(const char *action, int injected) {
    pt_json j;
    pt_event_begin(&j, "keyboard", action, "system_clipboard");
    if (injected)
        pt_put(&j, ",\"automated\":true");
    window_context(&j);
    DWORD generation = GetClipboardSequenceNumber();
    int opened = OpenClipboard(NULL), first = 1, truncated_files = 0;
    pt_put(&j, ",\"formats\":[");
    if (opened) {
        clipboard_format(&j, CF_UNICODETEXT, "text/plain", &first);
        clipboard_format(&j, html_format, "application/x-windows-cf-html", &first);
        clipboard_format(&j, rtf_format, "text/rtf", &first);
    }
    pt_put(&j, "],\"files\":[");
    if (opened) {
        HDROP files = (HDROP)GetClipboardData(CF_HDROP);
        if (files) {
            UINT count = DragQueryFileW(files, 0xFFFFFFFF, NULL, 0);
            first = 1;
            if (count > 1024) {
                count = 1024;
                truncated_files = 1;
            }
            for (UINT i = 0; i < count; i++) {
                UINT n = DragQueryFileW(files, i, NULL, 0);
                wchar_t *path = (wchar_t *)malloc(((size_t)n + 1) * sizeof(wchar_t));
                if (!path)
                    continue;
                DragQueryFileW(files, i, path, n + 1);
                char *value = utf8(path, (int)n);
                free(path);
                if (value) {
                    if (!first)
                        pt_put(&j, ",");
                    first = 0;
                    pt_string(&j, value);
                    free(value);
                }
            }
        }
        CloseClipboard();
    }
    pt_put(&j, "]");
    if (!opened)
        pt_property(&j, "issue", "clipboard is locked or unavailable");
    else if (truncated_files)
        pt_property(&j, "issue", "file reference count exceeds 1024; list truncated");
    else if (generation != GetClipboardSequenceNumber())
        pt_property(&j, "issue", "clipboard changed during capture");
    else if (pt_failure[0])
        pt_property(&j, "issue", pt_failure);
    pt_failure[0] = 0;
    pt_put(&j, ",\"captured_ns\":");
    pt_number(&j, pt_now());
    pt_put(&j, "}");
    pt_enqueue(pt_finish(&j));
}

static LRESULT CALLBACK keyboard(int code, WPARAM wparam, LPARAM lparam) {
    if (code == HC_ACTION && (wparam == WM_KEYDOWN || wparam == WM_SYSKEYDOWN)) {
        KBDLLHOOKSTRUCT *key = (KBDLLHOOKSTRUCT *)lparam;
        int control = (GetAsyncKeyState(VK_CONTROL) & 0x8000) != 0;
        int shift = (GetAsyncKeyState(VK_SHIFT) & 0x8000) != 0;
        int alt = (GetAsyncKeyState(VK_MENU) & 0x8000) != 0;
        int win = (GetAsyncKeyState(VK_LWIN) & 0x8000) || (GetAsyncKeyState(VK_RWIN) & 0x8000);
        if (!alt && !win) {
            if (key->vkCode == 'V' && control)
                capture(shift ? "ctrl+shift+v" : "ctrl+v", (key->flags & LLKHF_INJECTED) != 0);
            else if (key->vkCode == VK_INSERT && shift && !control)
                capture("shift+insert", (key->flags & LLKHF_INJECTED) != 0);
        }
    }
    return CallNextHookEx(keyboard_hook, code, wparam, lparam);
}

int pt_open(int limit) {
    pt_reset();
    pt_limit = limit;
    html_format = RegisterClipboardFormatW(L"HTML Format");
    rtf_format = RegisterClipboardFormatW(L"Rich Text Format");
    keyboard_hook = SetWindowsHookExW(WH_KEYBOARD_LL, keyboard, GetModuleHandleW(NULL), 0);
    if (!keyboard_hook) {
        snprintf(pt_failure, sizeof(pt_failure),
                 "could not install keyboard listener (Windows error %lu)", GetLastError());
        return 0;
    }
    return 1;
}

char *pt_next(int timeout_ms) {
    char *pending = pt_dequeue();
    if (pending)
        return pending;
    DWORD result = MsgWaitForMultipleObjects(0, NULL, FALSE, (DWORD)timeout_ms, QS_ALLINPUT);
    if (result == WAIT_FAILED) {
        snprintf(pt_failure, sizeof(pt_failure), "input message wait failed (%lu)", GetLastError());
        return NULL;
    }
    MSG message;
    while (PeekMessageW(&message, NULL, 0, 0, PM_REMOVE)) {
        if (message.message == WM_QUIT) {
            snprintf(pt_failure, sizeof(pt_failure), "input desktop message loop ended");
            break;
        }
        TranslateMessage(&message);
        DispatchMessageW(&message);
    }
    return pt_dequeue();
}

void pt_close(void) {
    if (keyboard_hook) {
        UnhookWindowsHookEx(keyboard_hook);
        keyboard_hook = NULL;
    }
    pt_reset();
}
