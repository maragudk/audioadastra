-- Tracks the app has indexed, one row per com.audioadastra.track record, keyed by its AT URI. A row
-- holds the version of the record with the given CID; a newer version (an edit) replaces it. The DID
-- is the record's author, which need not be a user of the app, so it does not reference users.
create table tracks (
  uri text primary key,
  did text not null,
  rkey text not null,
  cid text not null,
  title text not null,
  description text not null default '',
  -- created_at is the record's own createdAt, as its author declared it.
  created_at text not null,
  blob_cid text not null,
  blob_mime_type text not null,
  blob_size int not null,
  -- indexed_at is when the app indexed this version of the record.
  indexed_at text not null default (strftime('%Y-%m-%dT%H:%M:%fZ'))
) strict;

create index tracks_did_created_at_idx on tracks (did, created_at desc);
