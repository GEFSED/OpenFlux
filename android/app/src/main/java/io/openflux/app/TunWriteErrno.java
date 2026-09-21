package io.openflux.app;

import android.system.ErrnoException;
import java.io.IOException;

final class TunWriteErrno {
    static int from(IOException error) {
        Throwable cause = error;
        // Bound traversal even if a broken exception implementation forms a cycle.
        for (int i = 0; cause != null && i < 8; i++, cause = cause.getCause()) {
            if (cause instanceof ErrnoException) return ((ErrnoException) cause).errno;
        }
        // Some Android versions lose the typed cause. Match only the stable
        // errno token from FileOutputStream; never export the exception string.
        return fromMessage(error.getMessage());
    }

    static int fromMessage(String message) {
        return message != null && message.matches("(?s).*\\bEINVAL\\b.*")
                ? TunWriteDiagnostics.EINVAL : -1;
    }
}
