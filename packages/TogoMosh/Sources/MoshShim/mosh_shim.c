#include "mosh_shim.h"

#include <langinfo.h>
#include <locale.h>
#include <mosh/moshiosbridge.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static pthread_once_t prepare_once = PTHREAD_ONCE_INIT;
static int locale_is_utf8 = 0;

/// Process-wide state mosh reads; set once so concurrent sessions never race on it.
/// An empty MOSH_ESCAPE_KEY disables the escape key: Ctrl-^ . would quit the session
/// and Ctrl-^ Ctrl-Z would call pthread_exit on the client thread.
static void prepare_process(void) {
    setenv("MOSH_ESCAPE_KEY", "", 1);
    setlocale(LC_CTYPE, "UTF-8");
    const char *codeset = nl_langinfo(CODESET);
    locale_is_utf8 = codeset != NULL && strcmp(codeset, "UTF-8") == 0;
}

int togo_mosh_run(int in_fd, int out_fd, struct winsize *window_size,
                  const char *ip, const char *port, const char *key,
                  const char *predict_mode) {
    pthread_once(&prepare_once, prepare_process);
    if (!locale_is_utf8) {
        close(in_fd);
        close(out_fd);
        return TOGO_MOSH_ERR_LOCALE;
    }
    FILE *f_in = fdopen(in_fd, "r");
    FILE *f_out = fdopen(out_fd, "w");
    if (f_in == NULL || f_out == NULL) {
        if (f_in != NULL) fclose(f_in); else close(in_fd);
        if (f_out != NULL) fclose(f_out); else close(out_fd);
        return TOGO_MOSH_ERR_PIPE;
    }
    // mosh never calls fflush, so a buffered stream would hold frames back.
    setvbuf(f_out, NULL, _IONBF, 0);
    int rc = mosh_main(f_in, f_out, window_size, NULL, NULL, ip, port, key,
                       predict_mode, "", 0, NULL);
    fclose(f_in);
    fclose(f_out);
    return rc;
}

int togo_mosh_signal(pthread_t thread, int sig) {
    return pthread_kill(thread, sig);
}
