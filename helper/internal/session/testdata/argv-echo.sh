#!/bin/sh
# Writes each argument followed by a NUL byte to argv.out in the working
# directory, through a temporary file so a reader never sees half of it.
printf '%s\0' "$@" > argv.out.tmp && mv argv.out.tmp argv.out
