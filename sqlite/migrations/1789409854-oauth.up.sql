-- Users are identified by their atproto DID. Email login and its tokens go away, and so does the
-- accounts table above users. users_roles is dropped and recreated so its foreign key keeps pointing
-- at the new users table. Existing users are dropped with it: no login was ever possible before this
-- migration, so there are none.

drop table tokens;
drop table users_roles;
drop table users;
drop table accounts;

create table users (
  id text primary key default ('u_' || lower(hex(randomblob(16)))),
  created text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  updated text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  did text unique not null,
  active int not null default 1 check ( active in (0, 1) )
) strict;

create trigger users_updated_timestamp after update on users begin
  update users set updated = strftime('%Y-%m-%dT%H:%M:%fZ') where id = old.id;
end;

create table users_roles (
  user_id text not null references users (id) on delete cascade,
  role text not null references roles (role) on delete cascade,
  primary key (user_id, role)
) strict;

create index users_roles_role_idx on users_roles (role);

-- Pending OAuth authorization requests, keyed by the random state token. A row lives from the
-- pushed authorization request until the flow finishes, or until it is swept as stale.
create table oauth_auth_requests (
  state text primary key,
  created text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  auth_server_url text not null,
  account_did text,
  scopes text not null,
  request_uri text not null,
  auth_server_token_endpoint text not null,
  auth_server_revocation_endpoint text not null default '',
  pkce_verifier text not null,
  dpop_auth_server_nonce text not null,
  dpop_private_key_multibase text not null
) strict;

create index oauth_auth_requests_created_idx on oauth_auth_requests (created);

-- Established OAuth sessions. An account can have several (one per device), told apart by session_id.
create table oauth_sessions (
  did text not null,
  session_id text not null,
  created text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  updated text not null default (strftime('%Y-%m-%dT%H:%M:%fZ')),
  host_url text not null,
  auth_server_url text not null,
  auth_server_token_endpoint text not null,
  auth_server_revocation_endpoint text not null default '',
  scopes text not null,
  access_token text not null,
  refresh_token text not null,
  dpop_auth_server_nonce text not null,
  dpop_host_nonce text not null,
  dpop_private_key_multibase text not null,
  primary key (did, session_id)
) strict;

create trigger oauth_sessions_updated_timestamp after update on oauth_sessions begin
  update oauth_sessions set updated = strftime('%Y-%m-%dT%H:%M:%fZ') where did = old.did and session_id = old.session_id;
end;
