#ifndef MOSH_SHIM_H
#define MOSH_SHIM_H

#include <pthread.h>
#include <stddef.h>
#include <sys/ioctl.h>

/// fdopen failed for one of the pipes.
#define TOGO_MOSH_ERR_PIPE (-1)
/// The process could not switch LC_CTYPE to UTF-8; mosh would call exit(1).
#define TOGO_MOSH_ERR_LOCALE (-2)

/// Runs mosh_main on the calling thread until the session ends.
///
/// in_fd is the read end of the keystroke pipe; EOF on it starts a clean shutdown.
/// out_fd is the write end of the screen pipe. This call owns both and closes them
/// before it returns. window_size must stay valid until then: write new values into
/// it and send SIGWINCH to this thread to resize.
int togo_mosh_run(int in_fd, int out_fd, struct winsize *window_size,
                  const char *ip, const char *port, const char *key,
                  const char *predict_mode);

int togo_mosh_signal(pthread_t thread, int sig);

#endif
