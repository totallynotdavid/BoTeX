package message

import (
	"context"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
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
		Conversation: proto.String(text),
	})

	return err
}

func (ms *MessageSender) SendImage(ctx context.Context, recipient types.JID, imageData []byte, caption string) error {
	resp, err := ms.client.Upload(ctx, imageData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("failed to upload image: %w", err)
	}

	imageMsg := &waE2E.ImageMessage{
		Mimetype:      proto.String("image/jpeg"),
		URL:           &resp.URL,
		DirectPath:    &resp.DirectPath,
		MediaKey:      resp.MediaKey,
		FileEncSHA256: resp.FileEncSHA256,
		FileSHA256:    resp.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(imageData))),
	}

	if caption != "" {
		imageMsg.Caption = proto.String(caption)
	}

	_, err = ms.client.SendMessage(ctx, recipient, &waE2E.Message{
		ImageMessage: imageMsg,
	})

	return err
}

func (ms *MessageSender) SendSticker(ctx context.Context, recipient types.JID, stickerData []byte) error {
	resp, err := ms.client.Upload(ctx, stickerData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("failed to upload sticker: %w", err)
	}

	stickerMsg := &waE2E.StickerMessage{
		Mimetype:      proto.String("image/webp"),
		URL:           &resp.URL,
		DirectPath:    &resp.DirectPath,
		MediaKey:      resp.MediaKey,
		FileEncSHA256: resp.FileEncSHA256,
		FileSHA256:    resp.FileSHA256,
	}

	_, err = ms.client.SendMessage(ctx, recipient, &waE2E.Message{
		StickerMessage: stickerMsg,
	})

	return err
}

func (ms *MessageSender) SendDocument(ctx context.Context, recipient types.JID, documentData []byte, filename string, mimetype string) error {
	resp, err := ms.client.Upload(ctx, documentData, whatsmeow.MediaDocument)
	if err != nil {
		return fmt.Errorf("failed to upload document: %w", err)
	}

	documentMsg := &waE2E.DocumentMessage{
		Mimetype:      proto.String(mimetype),
		URL:           &resp.URL,
		DirectPath:    &resp.DirectPath,
		MediaKey:      resp.MediaKey,
		FileEncSHA256: resp.FileEncSHA256,
		FileSHA256:    resp.FileSHA256,
		FileName:      proto.String(filename),
		FileLength:    proto.Uint64(uint64(len(documentData))),
	}

	_, err = ms.client.SendMessage(ctx, recipient, &waE2E.Message{
		DocumentMessage: documentMsg,
	})

	return err
}

func (ms *MessageSender) SendVideo(ctx context.Context, recipient types.JID, videoData []byte, caption string) error {
	resp, err := ms.client.Upload(ctx, videoData, whatsmeow.MediaVideo)
	if err != nil {
		return fmt.Errorf("failed to upload video: %w", err)
	}

	videoMsg := &waE2E.VideoMessage{
		Mimetype:      proto.String("video/mp4"),
		URL:           &resp.URL,
		DirectPath:    &resp.DirectPath,
		MediaKey:      resp.MediaKey,
		FileEncSHA256: resp.FileEncSHA256,
		FileSHA256:    resp.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(videoData))),
	}

	if caption != "" {
		videoMsg.Caption = proto.String(caption)
	}

	_, err = ms.client.SendMessage(ctx, recipient, &waE2E.Message{
		VideoMessage: videoMsg,
	})

	return err
}

func (ms *MessageSender) SendAudio(ctx context.Context, recipient types.JID, audioData []byte) error {
	resp, err := ms.client.Upload(ctx, audioData, whatsmeow.MediaAudio)
	if err != nil {
		return fmt.Errorf("failed to upload audio: %w", err)
	}

	audioMsg := &waE2E.AudioMessage{
		Mimetype:      proto.String("audio/mp4"),
		URL:           &resp.URL,
		DirectPath:    &resp.DirectPath,
		MediaKey:      resp.MediaKey,
		FileEncSHA256: resp.FileEncSHA256,
		FileSHA256:    resp.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(audioData))),
	}

	_, err = ms.client.SendMessage(ctx, recipient, &waE2E.Message{
		AudioMessage: audioMsg,
	})

	return err
}

func (ms *MessageSender) SendReaction(ctx context.Context, recipient types.JID, messageID string, emoji string) error {
	_, err := ms.client.SendMessage(ctx, recipient, &waE2E.Message{
		ReactionMessage: &waE2E.ReactionMessage{
			Key: &waCommon.MessageKey{
				RemoteJID:   proto.String(recipient.String()),
				FromMe:      proto.Bool(true),
				ID:          proto.String(messageID),
				Participant: proto.String(recipient.String()),
			},
			Text: proto.String(emoji),
		},
	})

	return err
}
