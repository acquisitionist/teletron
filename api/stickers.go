package api

import (
	"encoding/json"
	"github.com/acquisitionist/teletron/internal/query"
	"net/url"
)

// Sticker represents a sticker.
type Sticker struct {
	Thumbnail        *PhotoSize     `json:"thumbnail,omitempty"`
	MaskPosition     *MaskPosition  `json:"mask_position,omitempty"`
	Type             StickerSetType `json:"type"`
	FileUniqueID     string         `json:"file_unique_id"`
	SetName          string         `json:"set_name,omitempty"`
	FileID           string         `json:"file_id"`
	Emoji            string         `json:"emoji,omitempty"`
	CustomEmojiID    string         `json:"custom_emoji_id,omitempty"`
	PremiumAnimation File           `json:"premium_animation,omitempty"`
	FileSize         int            `json:"file_size,omitempty"`
	Width            int            `json:"width"`
	Height           int            `json:"height"`
	IsVideo          bool           `json:"is_video"`
	IsAnimated       bool           `json:"is_animated"`
	NeedsRepainting  bool           `json:"needs_repainting,omitempty"`
}

// StickerSet represents a sticker set.
type StickerSet struct {
	Thumbnail   *PhotoSize     `json:"thumbnail,omitempty"`
	Title       string         `json:"title"`
	Name        string         `json:"name"`
	StickerType StickerSetType `json:"sticker_type"`
	Stickers    []Sticker      `json:"stickers"`
}

// StickerSetType represents the type of a sticker or of the entire set
type StickerSetType string

const (
	RegularStickerSet     StickerSetType = "regular"
	MaskStickerSet                       = "mask"
	CustomEmojiStickerSet                = "custom_emoji"
)

// StickerFormat is a custom type for the various sticker formats.
type StickerFormat string

// These are all the possible sticker formats.
const (
	StaticFormat   StickerFormat = "static"
	AnimatedFormat               = "animated"
	VideoFormat                  = "video"
)

// MaskPosition describes the position on faces where a mask should be placed by default.
type MaskPosition struct {
	Point  MaskPoint `json:"point"`
	XShift float32   `json:"x_shift"`
	YShift float32   `json:"y_shift"`
	Scale  float32   `json:"scale"`
}

// MaskPoint is a custom type for the various part of face where a mask should be placed.
type MaskPoint string

// These are all the possible parts of the face for a mask.
const (
	ForeheadPoint MaskPoint = "forehead"
	EyesPoint               = "eyes"
	MouthPoint              = "mouth"
	ChinPoint               = "chin"
)

// NewStickerSetOptions contains the optional parameters used in the CreateNewStickerSet method.
type NewStickerSetOptions struct {
	StickerType     StickerSetType `query:"sticker_type"`
	NeedsRepainting bool           `query:"needs_repainting"`
}

// InputSticker is a struct which describes a sticker to be added to a sticker set.
type InputSticker struct {
	MaskPosition *MaskPosition `json:"mask_position,omitempty"`
	Keywords     *[]string     `json:"keywords,omitempty"`
	Format       StickerFormat `json:"format"`
	Sticker      InputFile     `json:"-"`
	EmojiList    []string      `json:"emoji_list"`
}

// stickerEnvelope is a generic struct for all the various structs under the InputSticker interface.
type stickerEnvelope struct {
	Sticker string `json:"sticker"`
	InputSticker
}

// SendSticker is used to send static .WEBP or animated .TGS stickers.
func (a *API) SendSticker(stickerID string, chatID int64, opts *StickerOptions) (res ResponseMessage, err error) {
	var vals = make(url.Values)

	vals.Set("sticker", stickerID)
	vals.Set("chat_id", itoa(chatID))
	return res, a.get("sendSticker", query.AddValues(vals, opts), &res)
}

// GetStickerSet is used to get a sticker set.
func (a *API) GetStickerSet(name string) (res ResponseStickerSet, err error) {
	var vals = make(url.Values)

	vals.Set("name", name)
	return res, a.get("getStickerSet", vals, &res)
}

// GetCustomEmojiStickers is used to get information about custom emoji stickers by their identifiers.
func (a *API) GetCustomEmojiStickers(customEmojiIDs ...string) (res ResponseStickers, err error) {
	var vals = make(url.Values)

	jsn, _ := json.Marshal(customEmojiIDs)
	vals.Set("custom_emoji_ids", string(jsn))
	return res, a.get("getCustomEmojiStickers", vals, &res)
}

// UploadStickerFile is used to upload a .PNG file with a sticker for later use in
// CreateNewStickerSet and AddStickerToSet methods (can be used multiple times).
func (a *API) UploadStickerFile(userID int64, sticker InputFile, format StickerFormat) (res ResponseFile, err error) {
	var vals = make(url.Values)

	vals.Set("user_id", itoa(userID))
	vals.Set("sticker_format", string(format))
	return res, a.postFile("uploadStickerFile", "sticker", sticker, InputFile{}, vals, &res)
}

// CreateNewStickerSet is used to create a new sticker set owned by a user.
func (a *API) CreateNewStickerSet(userID int64, name, title string, stickers []InputSticker, opts *NewStickerSetOptions) (res ResponseBool, err error) {
	var vals = make(url.Values)

	vals.Set("user_id", itoa(userID))
	vals.Set("name", name)
	vals.Set("title", title)
	return res, a.postStickers("createNewStickerSet", query.AddValues(vals, opts), &res, stickers...)
}

// AddStickerToSet is used to add a new sticker to a set created by the bot.
func (a *API) AddStickerToSet(userID int64, name string, sticker InputSticker) (res ResponseBool, err error) {
	var vals = make(url.Values)

	vals.Set("user_id", itoa(userID))
	vals.Set("name", name)
	return res, a.postStickers("addStickerToSet", vals, &res, sticker)
}

// SetStickerPositionInSet is used to move a sticker in a set created by the bot to a specific position.
func (a *API) SetStickerPositionInSet(sticker string, position int) (res ResponseBase, err error) {
	var vals = make(url.Values)

	vals.Set("sticker", sticker)
	vals.Set("position", itoa(int64(position)))
	return res, a.get("setStickerPositionInSet", vals, &res)
}

// DeleteStickerFromSet is used to delete a sticker from a set created by the bot.
func (a *API) DeleteStickerFromSet(sticker string) (res ResponseBase, err error) {
	var vals = make(url.Values)

	vals.Set("sticker", sticker)
	return res, a.get("deleteStickerFromSet", vals, &res)
}

// ReplaceStickerInSet is used to replace an existing sticker in a sticker set with a new one.
// The method is equivalent to calling DeleteStickerFromSet, then AddStickerToSet, then SetStickerPositionInSet.
func (a *API) ReplaceStickerInSet(userID int64, name string, oldSticker string, sticker InputSticker) (res ResponseBool, err error) {
	var vals = make(url.Values)

	vals.Set("user_id", itoa(userID))
	vals.Set("name", name)
	vals.Set("old_sticker", oldSticker)
	return res, a.postStickers("replaceStickerInSet", vals, &res, sticker)
}

// SetStickerEmojiList is used to change the list of emoji assigned to a regular or custom emoji sticker.
// The sticker must belong to a sticker set created by the bot.
func (a *API) SetStickerEmojiList(sticker string, emojis []string) (res ResponseBool, err error) {
	var vals = make(url.Values)

	jsn, _ := json.Marshal(emojis)

	vals.Set("sticker", sticker)
	vals.Set("emoji_list", string(jsn))
	return res, a.get("setStickerEmojiList", vals, &res)
}

// SetStickerKeywords is used to change search keywords assigned to a regular or custom emoji sticker.
// The sticker must belong to a sticker set created by the bot.
func (a *API) SetStickerKeywords(sticker string, keywords []string) (res ResponseBool, err error) {
	var vals = make(url.Values)

	jsn, _ := json.Marshal(keywords)

	vals.Set("sticker", sticker)
	vals.Set("keywords", string(jsn))
	return res, a.get("setStickerKeywords", vals, &res)
}

// SetStickerMaskPosition is used to change the mask position of a mask sticker.
// The sticker must belong to a sticker set that was created by the bot.
func (a *API) SetStickerMaskPosition(sticker string, mask MaskPosition) (res ResponseBool, err error) {
	var vals = make(url.Values)

	jsn, _ := json.Marshal(mask)

	vals.Set("sticker", sticker)
	vals.Set("mask_position", string(jsn))
	return res, a.get("setStickerMaskPosition", vals, &res)
}

// SetStickerSetTitle is used to set the title of a created sticker set.
func (a *API) SetStickerSetTitle(name, title string) (res ResponseBool, err error) {
	var vals = make(url.Values)

	vals.Set("name", name)
	vals.Set("title", title)
	return res, a.get("setStickerSetTitle", vals, &res)
}

// SetStickerSetThumbnail is used to set the thumbnail of a sticker set.
func (a *API) SetStickerSetThumbnail(name string, userID int64, thumbnail InputFile, format StickerFormat) (res ResponseBase, err error) {
	var vals = make(url.Values)

	vals.Set("name", name)
	vals.Set("user_id", itoa(userID))
	vals.Set("format", string(format))
	return res, a.postFile("setStickerSetThumbnail", "thumbnail", thumbnail, InputFile{}, vals, &res)
}

// SetCustomEmojiStickerSetThumbnail is used to set the thumbnail of a custom emoji sticker set.
func (a *API) SetCustomEmojiStickerSetThumbnail(name, emojiID string) (res ResponseBool, err error) {
	var vals = make(url.Values)

	vals.Set("name", name)
	vals.Set("custom_emoji_id", emojiID)
	return res, a.get("setCustomEmojiStickerSetThumbnail", vals, &res)
}

// DeleteStickerSet is used to delete a sticker set that was created by the bot.
func (a *API) DeleteStickerSet(name string) (res ResponseBool, err error) {
	var vals = make(url.Values)

	vals.Set("name", name)
	return res, a.get("DeleteStickerSet", vals, &res)
}

// GetForumTopicIconStickers is used to get custom emoji stickers, which can be used as a forum topic icon by any user.
func (a *API) GetForumTopicIconStickers() (res ResponseStickers, err error) {
	return res, a.get("getForumTopicIconStickers", nil, &res)
}
