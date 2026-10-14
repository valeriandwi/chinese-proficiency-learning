package speech

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// WAV16kMono converts an uploaded recording to the 16 kHz mono 16-bit PCM WAV that
// pronunciation services expect.
//
// Conversion happens server-side on purpose. The browser records WebM/Opus because
// that is what MediaRecorder produces; a vendor's preferred container is not the
// client's problem, and asking the client to transcode would mean shipping an encoder
// (or a WebAssembly codec) to every learner to satisfy one API. ffmpeg is already a
// dependency of the audio pipeline, so this reuses it.
//
// The bytes are piped through stdin/stdout: nothing is written to disk, so a
// recording cannot survive the request even by accident. That is the retention
// promise the UI makes.
func WAV16kMono(ctx context.Context, ffmpegPath string, audio []byte) ([]byte, error) {
	if len(audio) == 0 {
		return nil, ErrEmptyAudio
	}
	if strings.TrimSpace(ffmpegPath) == "" {
		return nil, fmt.Errorf("speech: FFMPEG_PATH is not set, so the upload cannot be converted")
	}

	cmd := exec.CommandContext(ctx, ffmpegPath,
		// -hide_banner/-loglevel error: a learner's recording should not put ffmpeg
		// chatter in the server log.
		"-hide_banner", "-loglevel", "error",
		"-i", "pipe:0",
		"-ac", "1", "-ar", "16000", "-sample_fmt", "s16",
		"-f", "wav", "pipe:1",
	)
	cmd.Stdin = bytes.NewReader(audio)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("speech: ffmpeg not found at %q", ffmpegPath)
		}
		// The stderr tail is about the container, not the learner, so it is safe to
		// include a little of it — it is the difference between "unsupported audio"
		// and a mystery.
		tail := strings.TrimSpace(errOut.String())
		if len(tail) > 200 {
			tail = tail[len(tail)-200:]
		}
		return nil, fmt.Errorf("%w: ffmpeg could not decode it: %s", ErrUnsupportedAudio, tail)
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("%w: ffmpeg produced no audio", ErrUnsupportedAudio)
	}
	// A WAV header alone is 44 bytes; anything at that size is a decoding artefact,
	// not an utterance.
	if out.Len() <= 44 {
		return nil, ErrEmptyAudio
	}
	return out.Bytes(), nil
}
