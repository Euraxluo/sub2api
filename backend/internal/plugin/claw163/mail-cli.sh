#!/bin/sh
export LD_LIBRARY_PATH=/opt/c163/lib
exec /opt/c163/mail-cli "$@"
