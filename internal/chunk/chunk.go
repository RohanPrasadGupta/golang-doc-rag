package chunk

func SplitText(text string, chunkSize int, overlap int) []string {

	runes := []rune(text)
	chunks := []string{}
	step := chunkSize - overlap

	for i := 0; i < len(runes); i += step {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}
