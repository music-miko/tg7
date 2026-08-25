# Changelog

## Unreleased

### Fixed

- **Broadcast to users no longer treats every DM as a hard failure**
  (`src/handlers/broadcast.go`) — `/broadcast -user` was dumping thousands
  of entries into the error report, almost all `Chat not found`,
  `Have no write access to the chat`, or `Bot can't initiate conversation
  with a user`. Two causes: (1) `isChatGoneError`'s existing `"chat not
  found"` check was case-sensitive and never matched TDLib's actual
  `"Chat not found"` wording, and (2) `isUserGoneError` had no matcher at
  all for these three TDLib-specific error strings, which don't carry
  MTProto codes like `USER_IS_BLOCKED`. Both matchers now compare
  case-insensitively, and `isUserGoneError` recognizes all three as
  "can't reach this user" - they're now marked and skipped (excluded from
  future broadcasts via `GetActiveUsers`) instead of piling up in the
  error file every run.

### New

- **Spotify now resolves via the ArcMusic API** (`src/core/dl/arcspotify.go`)
  — Spotify track and playlist links are resolved through ArcMusic's
  `/spotify/playlist` and `/spotify/download` endpoints (same `ARC_API_URL` /
  `ARC_API_KEY` already used for YouTube in `arcmusic.go`, no new config
  needed) instead of always going through the generic `API_URL` gateway.
  `/spotify/download` returns an already-playable CDN link directly, so this
  path skips the AES decrypt / OGG rebuild / ffmpeg re-encode steps that the
  generic Spotify flow (`spotify_dl.go`) needs. Spotify album/artist links
  aren't supported by these endpoints and still fall through to the generic
  `apiData` (`API_URL`) client, same as when `ARC_API_URL` isn't configured.

- **Apple Music and JioSaavn now resolve via the ArcMusic API**
  (`src/core/dl/arcapplemusic.go`, `src/core/dl/arcjiosaavn.go`) — same
  pattern as the Spotify change above: song/album/playlist links for both
  platforms are now resolved through ArcMusic's dedicated
  `/applemusic/search` + `/applemusic/download` and `/jiosaavn/search` +
  `/jiosaavn/download` endpoints (same `ARC_API_URL` / `ARC_API_KEY`, no new
  config) instead of always going through the generic `apiData` (`API_URL`)
  gateway. Both `/download` endpoints already hand back a ready-to-stream
  CDN link (Apple Music via aplmate.com resolution, JioSaavn via server-side
  DES decrypt + 320kbps upgrade), so both paths skip straight to
  `processDirectDL` — no local decrypt step needed. Collection links
  (album/playlist/featured) resolve their track listing via `/search` and
  each track is re-resolved to a playable CDN link individually as it comes
  up for playback, exactly like `arcSpotify.getPlaylistInfo`. Wired into
  `NewDownloaderWrapper`'s selection order right after Spotify; anything
  these two clients don't recognize still falls through to the generic
  `apiData` client, same as before.

- **Bot API 10.3 "Button Revolution" — native in-message buttons**
  (`src/handlers/richtext.go`) — Rich Messages can now carry buttons as part
  of their own content (`inputPageBlockButtonRow`, with real
  default/primary/success/danger/link styles) instead of only ever trailing
  the message as a separate `reply_markup` keyboard. Added `RichButton`,
  `buttonRow`, `buttonRichMessage`, `sendButtonRich`, `replyButtonRich`, and
  `editButtonRichByID` to build and send these. The existing HTML-based rich
  message path (`richHTML` / `sendRich` / `editRich`, used by the bigger
  screens like Help and the Setup Guide) is untouched — HTML has no button
  syntax, so native in-message buttons only exist on this new block-based
  path, and both continue to coexist. `/autoplay`'s toggle panel is the
  first thing converted over, as a concrete example: the ON/OFF button now
  lives natively in the message (styled success/danger to match its state)
  instead of in a trailing keyboard. Also added `documentBlock` /
  `buttonRichMessageWithDocument` for the other half of Bot API 10.3 -
  attaching a document directly inside a rich message's content
  (`inputPageBlockDocument`) - available for handlers that want to pair a
  generated file with an explanatory blurb and action buttons in one
  message; expandable quote blocks and compact tables were already
  reachable through the existing HTML path (`<blockquote expandable>`,
  `<table compact bordered striped>`) and didn't need new plumbing.

  **Requires a `gotdbot` bump** (`go.mod`): the previously pinned commit
  (June 23) predates TDLib's button-row / expandable-quote-block support,
  which only landed upstream today (TDLib 1.8.67, gotdbot commit `b0fee9d`
  / `63e69eb`). `go.mod` now points at
  `v0.9.4-0.20260825024158-63e69ebc50e6`; run `go mod tidy` (network access
  to `github.com` required) after pulling to regenerate real `go.sum`
  hashes - the checked-in entries are placeholders and will fail `go build`
  as-is.

### Removed

- **Direct-DB media lookup (`src/core/dl/media_db.go`)** — the bot no longer
  opens its own MongoDB connection to the shared ArcMusic `arcapi.medias`
  collection to shortcut Telegram-channel cache hits. The ArcMusic API's
  `/youtube/v2/download` endpoint now performs this same cache check itself
  and resolves inline (no `job_id`, no polling) when a track is already
  cached — either as a public `https://t.me/<username>/<msg_id>` link or a
  CDN URL — so the bot's copy of this logic was redundant. `DB_URI` and
  `MEDIA_CHANNEL_ID` are no longer read from the environment.
- `arcMusic.createJob` / `arcJobResponse` (`src/core/dl/arcmusic.go`) —
  replaced by `requestDownload` / `arcDownloadResponse`, which understands
  the API's unified cache-hit-or-queued response instead of always expecting
  a job to be created. `pollJob` now reads the job result's `cdn` field
  instead of `public_url`.

## v1.1.0 — Autoplay, Force-Play, and Rich Navigation

### New

- **`/autoplay`** — toggles autoplay for the current chat via a panel with an
  ON/OFF button. When on, the bot picks a related YouTube track once the
  queue runs dry (backed by the same "Mix" playlist YouTube uses for its own
  `RD...` radio mixes) instead of leaving the voice chat idle. Turns itself
  off automatically when `/stop` / `/end` fully stops playback.
- **`/fplay`, `/fp`** — force-play: same as `/play`, but cuts the track to
  the front of the queue (right after whatever's currently playing) instead
  of appending it to the end. Admin/authorized-user only.
- **`/fvplay`, `/fvp`** — the force-play variant of `/vplay`.
- **Queue limit raised from 10 to 25 tracks** (`MaxQueueLength` in
  `src/handlers/play.go`), with a clearer "queue full" message that also
  points at `/remove`.
- **Rewrote the empty `/play` / `/vplay` / `/fplay` / `/fvplay` reply** — now
  a Rich HTML table of "what you have → what to run" instead of a bullet
  list, plus a collapsed "See also" pointing at the force-play and autoplay
  commands.
- **Rewrote the Setup Guide** — added a "Common questions" section (why the
  bot needs an assistant account, what to check when nothing plays, how to
  turn on autoplay) alongside the existing stepper and admin-rights blocks,
  and the command reference table now includes `/fplay` and `/autoplay`.

### Fixed

- **Private `/start` no longer opens Help / Setup Guide by deleting and
  resending a message.** The welcome screen used to be a photo message
  (image + caption), which meant navigating to Help or the Setup Guide had
  to delete that photo and send a fresh Rich Message, and "Back" had to
  delete *that* and recreate the photo. The welcome image is now embedded
  directly in the Rich Message via `<img src="...">`, so `/start` → Help →
  a category → Setup Guide → Back is a plain in-place edit the whole way,
  in both private chats and groups. `promoteToRich` / `demoteToPhoto` and
  the `isPhoto` branching they required have been removed as a result.

## v1.0.0 — Initial Arc Release

### Changes

- **Rebranded** from Fallen to Team Arc; support links updated to
  https://t.me/arcchatz (chat) and https://t.me/ArcUpdates (channel).

- **`/start` command** — private DM and group text now match the tosu4 style,
  with a platform list and concise uptime display.

- **Setup Guide button** — added to both the private DM keyboard and the group
  `/start` keyboard; shows a 4-step group setup guide with Back / Close buttons.

- **Arc API YouTube downloader** (`src/core/dl/arc_api.go`)
  - YouTube audio and video downloads exclusively use the Arc API
    (`/youtube/v2/download` → `/youtube/jobStatus`) with the same
    job-queue retry logic as `_api.py`.
  - All other platforms (Spotify, Apple Music, SoundCloud, Deezer, JioSaavn,
    Tidal, MXPlayer, Twitch, Kick …) continue to use the original API gateway
    path unchanged.

- **DB channel cache** (`arcCache` inside `arc_api.go`)
  - Owns a **separate** MongoDB connection using `DB_URI` (mirrors `_api.py`'s
    `Cache` class which uses `CACHE_DB`). Falls back to `MONGO_URI` if `DB_URI`
    is not set.
  - Database: `arcapi`, collection: `medias` — documents keyed by
    `(track_id, is_video)` storing `message_id`.
  - On each YouTube download request the cache is checked first; if a message
    is found the file is streamed directly from the Telegram media channel via
    the `DlBot` client, exactly as `_api.py` does via `track.download()`.
  - `SaveToDBCache(videoID, video, msgID)` is exposed so the calling code can
    write back to the cache after uploading a new file to `MEDIA_CHANNEL_ID`.

- **New env vars**
  - `MEDIA_CHANNEL_ID` — Telegram channel ID holding cached media files.
  - `DB_URI` — separate MongoDB URI for the Arc media cache; falls back to
    `MONGO_URI` if unset.

- **Default DB name** changed to `ArcMusicBot`.
