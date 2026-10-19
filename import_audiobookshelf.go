package audiobook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	sdk "github.com/beamfall/plugin-sdk"
)

const (
	// AudiobookshelfImporterID is the stable bundle-local Migration Wizard ID.
	AudiobookshelfImporterID = "audiobookshelf"
	// ImporterID is the domain-neutral composition-root name for this plugin's
	// sole Migration Wizard source.
	ImporterID = AudiobookshelfImporterID

	maxAudiobookshelfExportBytes = 64 << 20
	audiobookshelfPageItems      = 256
	audiobookshelfIDScheme       = "audiobookshelf"
)

var errAudiobookshelfExportTooLarge = errors.New("audiobookshelf export exceeds 64 MB size limit")

type audiobookshelfSource struct {
	path string
}

func (audiobookshelfSource) Categories() []string {
	return []string{sdk.CategoryWatchState}
}

func (s audiobookshelfSource) Items(ctx context.Context) ([]sdk.ForeignItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("open audiobookshelf export: %w", err)
	}
	defer func() { _ = f.Close() }()
	return ParseAudiobookshelfExport(f)
}

func (s audiobookshelfSource) Stream(
	ctx context.Context,
	checkpoint string,
	emit func(context.Context, []sdk.ForeignItem, string) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	kind, err := audiobookshelfRootKind(s.path)
	if err != nil {
		return err
	}
	pager, err := newAudiobookshelfPageEmitter(ctx, checkpoint, emit)
	if err != nil {
		return err
	}
	row := 0
	if kind == 0 || kind == 'n' {
		return pager.finish()
	}
	if kind == '[' {
		if err := streamAudiobookshelfArray(ctx, s.path, nil, pager, &row); err != nil {
			return err
		}
		return pager.finish()
	}
	if kind != '{' {
		return errors.New("audiobookshelf export must be a JSON array or object")
	}
	paths, progressUsers, err := inspectAudiobookshelfEnvelope(ctx, s.path)
	if err != nil {
		return err
	}
	if progressUsers > 1 {
		return errors.New("audiobookshelf export contains progress for multiple users")
	}
	for _, section := range []audiobookshelfProgressSection{
		audiobookshelfTopProgress,
		audiobookshelfSingleUser,
		audiobookshelfUsers,
	} {
		if err := streamAudiobookshelfEnvelopeSection(ctx, s.path, section, paths, pager, &row); err != nil {
			return err
		}
	}
	return pager.finish()
}

type audiobookshelfProgressSection uint8

const (
	audiobookshelfTopProgress audiobookshelfProgressSection = iota
	audiobookshelfSingleUser
	audiobookshelfUsers
)

func audiobookshelfRootKind(path string) (byte, error) {
	f, err := openAudiobookshelfImport(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	decoder := json.NewDecoder(io.LimitReader(f, maxAudiobookshelfExportBytes+1))
	token, err := decoder.Token()
	if err == io.EOF {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if token == nil {
		if err := audiobookshelfJSONEOF(decoder); err != nil {
			return 0, err
		}
		return 'n', nil
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return 0, errors.New("audiobookshelf export must be a JSON array or object")
	}
	return byte(delim), nil
}

func inspectAudiobookshelfEnvelope(
	ctx context.Context,
	path string,
) (map[string]string, int, error) {
	f, err := openAudiobookshelfImport(path)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = f.Close() }()
	decoder := json.NewDecoder(io.LimitReader(f, maxAudiobookshelfExportBytes+1))
	start, err := decoder.Token()
	if err != nil {
		return nil, 0, err
	}
	if start != json.Delim('{') {
		return nil, 0, errors.New("audiobookshelf export must be a JSON object")
	}
	libraryPaths := map[string]string{}
	itemPaths := map[string]string{}
	progressUsers := 0
	for decoder.More() {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		key, err := decoder.Token()
		if err != nil {
			return nil, 0, err
		}
		switch key {
		case "libraryItems":
			err = collectAudiobookshelfPaths(ctx, decoder, libraryPaths)
		case "items":
			err = collectAudiobookshelfPaths(ctx, decoder, itemPaths)
		case "users":
			progressUsers, err = countAudiobookshelfProgressUsers(ctx, decoder)
		default:
			err = skipAudiobookshelfJSONValue(decoder)
		}
		if err != nil {
			return nil, 0, err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, 0, err
	}
	if err := audiobookshelfJSONEOF(decoder); err != nil {
		return nil, 0, err
	}
	for id, path := range itemPaths {
		libraryPaths[id] = path
	}
	return libraryPaths, progressUsers, nil
}

func collectAudiobookshelfPaths(
	ctx context.Context,
	decoder *json.Decoder,
	paths map[string]string,
) error {
	start, err := decoder.Token()
	if err != nil {
		return err
	}
	if start == nil {
		return nil
	}
	if start != json.Delim('[') {
		return errors.New("audiobookshelf items must be an array")
	}
	for decoder.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var item audiobookshelfItem
		if err := decoder.Decode(&item); err != nil {
			return err
		}
		if item.ID != "" && item.Path != "" {
			paths[item.ID] = item.Path
		}
	}
	_, err = decoder.Token()
	return err
}

func countAudiobookshelfProgressUsers(ctx context.Context, decoder *json.Decoder) (int, error) {
	start, err := decoder.Token()
	if err != nil {
		return 0, err
	}
	if start == nil {
		return 0, nil
	}
	if start != json.Delim('[') {
		return 0, errors.New("audiobookshelf users must be an array")
	}
	users := 0
	for decoder.More() {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		hasProgress, err := audiobookshelfUserHasProgress(decoder)
		if err != nil {
			return 0, err
		}
		if hasProgress {
			users++
		}
	}
	_, err = decoder.Token()
	return users, err
}

func audiobookshelfUserHasProgress(decoder *json.Decoder) (bool, error) {
	start, err := decoder.Token()
	if err != nil {
		return false, err
	}
	if start == nil {
		return false, nil
	}
	if start != json.Delim('{') {
		return false, errors.New("audiobookshelf user must be an object")
	}
	hasProgress := false
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return false, err
		}
		if key == "mediaProgress" {
			var count int
			count, err = countAudiobookshelfArray(decoder)
			hasProgress = count > 0
		} else {
			err = skipAudiobookshelfJSONValue(decoder)
		}
		if err != nil {
			return false, err
		}
	}
	_, err = decoder.Token()
	return hasProgress, err
}

func countAudiobookshelfArray(decoder *json.Decoder) (int, error) {
	start, err := decoder.Token()
	if err != nil {
		return 0, err
	}
	if start == nil {
		return 0, nil
	}
	if start != json.Delim('[') {
		return 0, errors.New("audiobookshelf progress must be an array")
	}
	count := 0
	for decoder.More() {
		if err := skipAudiobookshelfJSONValue(decoder); err != nil {
			return 0, err
		}
		count++
	}
	_, err = decoder.Token()
	return count, err
}

func streamAudiobookshelfArray(
	ctx context.Context,
	path string,
	paths map[string]string,
	pager *audiobookshelfPageEmitter,
	row *int,
) error {
	f, err := openAudiobookshelfImport(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	decoder := json.NewDecoder(io.LimitReader(f, maxAudiobookshelfExportBytes+1))
	if err := streamAudiobookshelfProgress(ctx, decoder, paths, pager, row); err != nil {
		return err
	}
	return audiobookshelfJSONEOF(decoder)
}

func streamAudiobookshelfEnvelopeSection(
	ctx context.Context,
	path string,
	section audiobookshelfProgressSection,
	paths map[string]string,
	pager *audiobookshelfPageEmitter,
	row *int,
) error {
	f, err := openAudiobookshelfImport(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	decoder := json.NewDecoder(io.LimitReader(f, maxAudiobookshelfExportBytes+1))
	start, err := decoder.Token()
	if err != nil {
		return err
	}
	if start != json.Delim('{') {
		return errors.New("audiobookshelf export must be a JSON object")
	}
	for decoder.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		switch {
		case section == audiobookshelfTopProgress && key == "mediaProgress":
			err = streamAudiobookshelfProgress(ctx, decoder, paths, pager, row)
		case section == audiobookshelfSingleUser && key == "user":
			err = streamAudiobookshelfUser(ctx, decoder, paths, pager, row)
		case section == audiobookshelfUsers && key == "users":
			err = streamAudiobookshelfUsers(ctx, decoder, paths, pager, row)
		default:
			err = skipAudiobookshelfJSONValue(decoder)
		}
		if err != nil {
			return err
		}
	}
	if _, err = decoder.Token(); err != nil {
		return err
	}
	return audiobookshelfJSONEOF(decoder)
}

func audiobookshelfJSONEOF(decoder *json.Decoder) error {
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return errors.New("audiobookshelf export has a trailing JSON value")
		}
		return err
	}
	return nil
}

func streamAudiobookshelfUsers(
	ctx context.Context,
	decoder *json.Decoder,
	paths map[string]string,
	pager *audiobookshelfPageEmitter,
	row *int,
) error {
	start, err := decoder.Token()
	if err != nil {
		return err
	}
	if start == nil {
		return nil
	}
	if start != json.Delim('[') {
		return errors.New("audiobookshelf users must be an array")
	}
	for decoder.More() {
		if err := streamAudiobookshelfUser(ctx, decoder, paths, pager, row); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func streamAudiobookshelfUser(
	ctx context.Context,
	decoder *json.Decoder,
	paths map[string]string,
	pager *audiobookshelfPageEmitter,
	row *int,
) error {
	start, err := decoder.Token()
	if err != nil {
		return err
	}
	if start == nil {
		return nil
	}
	if start != json.Delim('{') {
		return errors.New("audiobookshelf user must be an object")
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		if key == "mediaProgress" {
			err = streamAudiobookshelfProgress(ctx, decoder, paths, pager, row)
		} else {
			err = skipAudiobookshelfJSONValue(decoder)
		}
		if err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func streamAudiobookshelfProgress(
	ctx context.Context,
	decoder *json.Decoder,
	paths map[string]string,
	pager *audiobookshelfPageEmitter,
	row *int,
) error {
	start, err := decoder.Token()
	if err != nil {
		return err
	}
	if start == nil {
		return nil
	}
	if start != json.Delim('[') {
		return errors.New("audiobookshelf progress must be an array")
	}
	for decoder.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var progress audiobookshelfProgress
		if err := decoder.Decode(&progress); err != nil {
			return err
		}
		(*row)++
		item, err := progress.foreignItem(paths)
		if err != nil {
			return fmt.Errorf("audiobookshelf progress row %d: %w", *row, err)
		}
		if err := pager.add(item); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func openAudiobookshelfImport(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audiobookshelf export: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.Size() > maxAudiobookshelfExportBytes {
		_ = f.Close()
		return nil, errAudiobookshelfExportTooLarge
	}
	return f, nil
}

type audiobookshelfPageEmitter struct {
	ctx    context.Context
	emit   func(context.Context, []sdk.ForeignItem, string) error
	resume int
	seen   int
	page   []sdk.ForeignItem
}

func newAudiobookshelfPageEmitter(
	ctx context.Context,
	checkpoint string,
	emit func(context.Context, []sdk.ForeignItem, string) error,
) (*audiobookshelfPageEmitter, error) {
	resume, err := importCheckpoint(checkpoint)
	if err != nil {
		return nil, err
	}
	return &audiobookshelfPageEmitter{
		ctx:    ctx,
		emit:   emit,
		resume: resume,
		page:   make([]sdk.ForeignItem, 0, audiobookshelfPageItems),
	}, nil
}

func (p *audiobookshelfPageEmitter) add(item sdk.ForeignItem) error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	p.seen++
	if p.seen <= p.resume {
		return nil
	}
	p.page = append(p.page, item)
	if len(p.page) < audiobookshelfPageItems {
		return nil
	}
	return p.flush()
}

func (p *audiobookshelfPageEmitter) finish() error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	if p.seen < p.resume {
		return fmt.Errorf("invalid audiobookshelf checkpoint %d: beyond source", p.resume)
	}
	return p.flush()
}

func (p *audiobookshelfPageEmitter) flush() error {
	if len(p.page) == 0 {
		return nil
	}
	if err := p.emit(p.ctx, p.page, strconv.Itoa(p.seen)); err != nil {
		return err
	}
	if err := p.ctx.Err(); err != nil {
		return err
	}
	p.page = make([]sdk.ForeignItem, 0, audiobookshelfPageItems)
	return nil
}

func skipAudiobookshelfJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		for decoder.More() {
			if _, err := decoder.Token(); err != nil {
				return err
			}
			if err := skipAudiobookshelfJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := skipAudiobookshelfJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errors.New("audiobookshelf export has an unexpected JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

func importCheckpoint(checkpoint string) (int, error) {
	if checkpoint == "" {
		return 0, nil
	}
	index, err := strconv.Atoi(checkpoint)
	if err != nil || index < 0 {
		return 0, fmt.Errorf("invalid audiobookshelf checkpoint %q", checkpoint)
	}
	return index, nil
}

// NewAudiobookshelfSource returns a Migration Wizard source for an
// Audiobookshelf user/progress JSON export.
func NewAudiobookshelfSource(path string) sdk.Source { //nolint:staticcheck // deliberate SDK 0.x compatibility path for pre-streaming consumers
	return audiobookshelfSource{path: path}
}

// NewImportSource is the domain-neutral composition-root constructor.
func NewImportSource(path string) sdk.Source { //nolint:staticcheck // deliberate SDK 0.x compatibility path for pre-streaming consumers
	return NewAudiobookshelfSource(path)
}

// MigrationSource lets Core discover this plugin's compiled importer through
// the same Parser value it already registers for scanning, without teaching
// the domain-agnostic composition root any source-system names.
func (*Parser) MigrationSource(importerID string, _ []string, path string) (sdk.Source, bool) { //nolint:staticcheck // deliberate SDK 0.x compatibility path for pre-streaming consumers
	if importerID != ImporterID {
		return nil, false
	}
	return NewImportSource(path), true
}

type audiobookshelfExport struct {
	MediaProgress []audiobookshelfProgress `json:"mediaProgress"`
	User          *audiobookshelfUser      `json:"user"`
	Users         []audiobookshelfUser     `json:"users"`
	LibraryItems  []audiobookshelfItem     `json:"libraryItems"`
	Items         []audiobookshelfItem     `json:"items"`
}

type audiobookshelfUser struct {
	MediaProgress []audiobookshelfProgress `json:"mediaProgress"`
}

type audiobookshelfItem struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type audiobookshelfProgress struct {
	LibraryItemID string          `json:"libraryItemId"`
	MediaItemID   string          `json:"mediaItemId"`
	Path          string          `json:"path"`
	MediaPath     string          `json:"mediaPath"`
	CurrentTime   float64         `json:"currentTime"`
	Duration      float64         `json:"duration"`
	IsFinished    bool            `json:"isFinished"`
	LastUpdate    json.RawMessage `json:"lastUpdate"`
}

// ParseAudiobookshelfExport parses either the MediaProgress array returned by
// Audiobookshelf or an export envelope containing mediaProgress. A server
// backup with more than one progress-bearing user is rejected: one Migration
// Wizard run targets one Beamfall Profile and must not merge users silently.
func ParseAudiobookshelfExport(r io.Reader) ([]sdk.ForeignItem, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxAudiobookshelfExportBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read audiobookshelf export: %w", err)
	}
	if len(data) > maxAudiobookshelfExportBytes {
		return nil, errAudiobookshelfExportTooLarge
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return []sdk.ForeignItem{}, nil
	}

	var progress []audiobookshelfProgress
	paths := map[string]string{}
	if data[0] == '[' {
		if err := json.Unmarshal(data, &progress); err != nil {
			return nil, fmt.Errorf("parse audiobookshelf progress array: %w", err)
		}
	} else {
		var export audiobookshelfExport
		if err := json.Unmarshal(data, &export); err != nil {
			return nil, fmt.Errorf("parse audiobookshelf export: %w", err)
		}
		progress = export.MediaProgress
		if export.User != nil {
			progress = append(progress, export.User.MediaProgress...)
		}
		progressUsers := 0
		for _, user := range export.Users {
			if len(user.MediaProgress) == 0 {
				continue
			}
			progressUsers++
			progress = append(progress, user.MediaProgress...)
		}
		if progressUsers > 1 {
			return nil, errors.New("audiobookshelf export contains progress for multiple users")
		}
		for _, item := range append(export.LibraryItems, export.Items...) {
			if item.ID != "" && item.Path != "" {
				paths[item.ID] = item.Path
			}
		}
	}

	items := make([]sdk.ForeignItem, 0, len(progress))
	for i, row := range progress {
		item, err := row.foreignItem(paths)
		if err != nil {
			return nil, fmt.Errorf("audiobookshelf progress row %d: %w", i+1, err)
		}
		items = append(items, item)
	}
	return items, nil
}

func (p audiobookshelfProgress) foreignItem(paths map[string]string) (sdk.ForeignItem, error) {
	id := p.LibraryItemID
	if id == "" {
		id = p.MediaItemID
	}
	path := p.Path
	if path == "" {
		path = p.MediaPath
	}
	if path == "" {
		path = paths[p.LibraryItemID]
	}
	if path == "" && id == "" {
		return sdk.ForeignItem{}, errors.New("missing path and library item id")
	}

	item := sdk.ForeignItem{
		Path: path,
		WatchState: sdk.WatchState{
			Present:         true,
			PositionSeconds: wholeSeconds(p.CurrentTime),
			DurationSeconds: wholeSeconds(p.Duration),
			Watched:         p.IsFinished,
		},
	}
	if id != "" {
		item.ExternalIDs = []sdk.ExternalID{{Scheme: audiobookshelfIDScheme, Value: id}}
	}
	if len(p.LastUpdate) > 0 && string(p.LastUpdate) != "null" {
		occurredAt, err := parseAudiobookshelfTime(p.LastUpdate)
		if err != nil {
			return sdk.ForeignItem{}, fmt.Errorf("lastUpdate: %w", err)
		}
		item.WatchState.OccurredAt = occurredAt
	}
	return item, nil
}

func wholeSeconds(value float64) int {
	if value <= 0 {
		return 0
	}
	maxInt := int(^uint(0) >> 1)
	if value >= float64(maxInt) {
		return maxInt
	}
	return int(value)
}

func parseAudiobookshelfTime(raw json.RawMessage) (time.Time, error) {
	var text string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return time.Time{}, err
		}
		if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return parsed, nil
		}
	} else {
		text = string(raw)
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("unsupported timestamp %q", text)
	}
	// Audiobookshelf timestamps are milliseconds since Unix epoch. Accept
	// second-resolution fixtures as well for older exports.
	if value > 10_000_000_000 {
		return time.UnixMilli(value), nil
	}
	return time.Unix(value, 0), nil
}
