package codersdk

// AudioTranscriptionResponse is returned by POST /api/v2/audio-transcriptions.
type AudioTranscriptionResponse struct {
	Text string `json:"text"`
}
