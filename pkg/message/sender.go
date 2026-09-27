package message

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

type MessageSender struct {
	client *whatsmeow.Client
}

func NewMessageSender(client *whatsmeow.Client) *MessageSender {
	return &MessageSender{
		client: client,
	}
}

func (ms *MessageSender) SendText(ctx context.Context, recipient types.JID, text string) error {
	_, err := ms.client.SendMessage(ctx, recipient, &waE2E.Message{
		Conversation: new(text),
	})
	if err != nil {
		return fmt.Errorf("failed to send text message: %w", err)
	}

	return nil
}

func (ms *MessageSender) SendImage(ctx context.Context, recipient types.JID, imageData []byte, caption string) error {
	resp, err := ms.client.Upload(ctx, imageData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("failed to upload image: %w", err)
	}

	imageMsg := &waE2E.ImageMessage{
		Mimetype:      new("image/jpeg"),
		URL:           &resp.URL,
		DirectPath:    &resp.DirectPath,
		MediaKey:      resp.MediaKey,
		FileEncSHA256: resp.FileEncSHA256,
		FileSHA256:    resp.FileSHA256,
		FileLength:    new(uint64(len(imageData))),
	}

	if caption != "" {
		imageMsg.Caption = new(caption)
	}

	_, err = ms.client.SendMessage(ctx, recipient, &waE2E.Message{
		ImageMessage: imageMsg,
	})
	if err != nil {
		return fmt.Errorf("failed to send image message: %w", err)
	}

	return nil
}

func (ms *MessageSender) SendSticker(ctx context.Context, recipient types.JID, stickerData []byte) error {
	resp, err := ms.client.Upload(ctx, stickerData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("failed to upload sticker: %w", err)
	}

	stickerMsg := &waE2E.StickerMessage{
		Mimetype:      new("image/webp"),
		URL:           &resp.URL,
		DirectPath:    &resp.DirectPath,
		MediaKey:      resp.MediaKey,
		FileEncSHA256: resp.FileEncSHA256,
		FileSHA256:    resp.FileSHA256,
	}

	_, err = ms.client.SendMessage(ctx, recipient, &waE2E.Message{
		StickerMessage: stickerMsg,
	})
	if err != nil {
		return fmt.Errorf("failed to send sticker message: %w", err)
	}

	return nil
}

func (ms *MessageSender) SendDocument(ctx context.Context, recipient types.JID, documentData []byte, filename, mimetype string) error {
	resp, err := ms.client.Upload(ctx, documentData, whatsmeow.MediaDocument)
	if err != nil {
		return fmt.Errorf("failed to upload document: %w", err)
	}

	documentMsg := &waE2E.DocumentMessage{
		Mimetype:      new(mimetype),
		URL:           &resp.URL,
		DirectPath:    &resp.DirectPath,
		MediaKey:      resp.MediaKey,
		FileEncSHA256: resp.FileEncSHA256,
		FileSHA256:    resp.FileSHA256,
		FileName:      new(filename),
		FileLength:    new(uint64(len(documentData))),
	}

	_, err = ms.client.SendMessage(ctx, recipient, &waE2E.Message{
		DocumentMessage: documentMsg,
	})
	if err != nil {
		return fmt.Errorf("failed to send document message: %w", err)
	}

	return nil
}

func (ms *MessageSender) SendVideo(ctx context.Context, recipient types.JID, videoData []byte, caption string) error {
	resp, err := ms.client.Upload(ctx, videoData, whatsmeow.MediaVideo)
	if err != nil {
		return fmt.Errorf("failed to upload video: %w", err)
	}

	videoMsg := &waE2E.VideoMessage{
		Mimetype:      new("video/mp4"),
		URL:           &resp.URL,
		DirectPath:    &resp.DirectPath,
		MediaKey:      resp.MediaKey,
		FileEncSHA256: resp.FileEncSHA256,
		FileSHA256:    resp.FileSHA256,
		FileLength:    new(uint64(len(videoData))),
	}

	if caption != "" {
		videoMsg.Caption = new(caption)
	}

	_, err = ms.client.SendMessage(ctx, recipient, &waE2E.Message{
		VideoMessage: videoMsg,
	})
	if err != nil {
		return fmt.Errorf("failed to send video message: %w", err)
	}

	return nil
}

func (ms *MessageSender) SendAudio(ctx context.Context, recipient types.JID, audioData []byte) error {
	resp, err := ms.client.Upload(ctx, audioData, whatsmeow.MediaAudio)
	if err != nil {
		return fmt.Errorf("failed to upload audio: %w", err)
	}

	audioMsg := &waE2E.AudioMessage{
		Mimetype:      new("audio/mp4"),
		URL:           &resp.URL,
		DirectPath:    &resp.DirectPath,
		MediaKey:      resp.MediaKey,
		FileEncSHA256: resp.FileEncSHA256,
		FileSHA256:    resp.FileSHA256,
		FileLength:    new(uint64(len(audioData))),
	}

	_, err = ms.client.SendMessage(ctx, recipient, &waE2E.Message{
		AudioMessage: audioMsg,
	})
	if err != nil {
		return fmt.Errorf("failed to send audio message: %w", err)
	}

	return nil
}

func (ms *MessageSender) SendReaction(ctx context.Context, recipient types.JID, messageID, emoji string) error {
	_, err := ms.client.SendMessage(ctx, recipient, &waE2E.Message{
		ReactionMessage: &waE2E.ReactionMessage{
			Key: &waCommon.MessageKey{
				RemoteJID:   new(recipient.String()),
				FromMe:      new(true),
				ID:          new(messageID),
				Participant: new(recipient.String()),
			},
			Text: new(emoji),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send reaction message: %w", err)
	}

	return nil
}
