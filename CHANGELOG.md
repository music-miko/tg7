# Changelog

## Unreleased

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
