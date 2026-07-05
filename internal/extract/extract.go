package extract

import (
	"bytes"
	"fmt"
	"os/exec"
)

func ExtractText(data []byte) (string, error) {

	cmd := exec.Command("pdftotext", "-", "-")

	cmd.Stdin = bytes.NewReader(data)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf(
			"pdftotext failed: %w: %s",
			err,
			stderr.String(),
		)
	}
	return out.String(), nil

}
