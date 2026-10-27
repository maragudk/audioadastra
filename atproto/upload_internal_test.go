package atproto

import (
	"context"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multibase"
	"github.com/multiformats/go-multihash"
	"hegel.dev/go/hegel"
	"maragu.dev/is"

	"app/model"
)

func TestServerCache(t *testing.T) {
	t.Run("should keep a description for a day per host", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var c serverCache
			f := &fetcher{description: model.ServerDescription{BlobUploadLimit: 42}}

			description, hit, err := c.get(t.Context(), "https://pds.test", f.fetch)
			is.NotError(t, err)
			is.True(t, !hit, "hit on the first get")
			is.Equal(t, int64(42), description.BlobUploadLimit)

			time.Sleep(24*time.Hour - time.Second)
			description, hit, err = c.get(t.Context(), "https://pds.test", f.fetch)
			is.NotError(t, err)
			is.True(t, hit, "miss within the day")
			is.Equal(t, int64(42), description.BlobUploadLimit)
			is.Equal(t, 1, f.calls)

			_, hit, _ = c.get(t.Context(), "https://other.test", f.fetch)
			is.True(t, !hit, "hit for another host")
			is.Equal(t, 2, f.calls)

			time.Sleep(time.Second)
			_, hit, _ = c.get(t.Context(), "https://pds.test", f.fetch)
			is.True(t, !hit, "hit after the day")
			is.Equal(t, 3, f.calls)
		})
	})

	t.Run("should keep a failure for a minute", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var c serverCache
			f := &fetcher{err: model.ErrorPDSUnavailable}

			_, hit, err := c.get(t.Context(), "https://pds.test", f.fetch)
			is.Error(t, model.ErrorPDSUnavailable, err)
			is.True(t, !hit, "hit on the first get")

			time.Sleep(time.Minute - time.Second)
			_, hit, err = c.get(t.Context(), "https://pds.test", f.fetch)
			is.Error(t, model.ErrorPDSUnavailable, err)
			is.True(t, hit, "miss within the minute")
			is.Equal(t, 1, f.calls)

			time.Sleep(time.Second)
			f.err = nil
			_, hit, err = c.get(t.Context(), "https://pds.test", f.fetch)
			is.NotError(t, err)
			is.True(t, !hit, "hit after the minute")
			is.Equal(t, 2, f.calls)
		})
	})

	t.Run("should not keep a fetch cut short by the caller", func(t *testing.T) {
		var c serverCache
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		f := &fetcher{err: context.Canceled}

		_, _, err := c.get(ctx, "https://pds.test", f.fetch)
		is.Error(t, context.Canceled, err)

		f.err = nil
		_, hit, err := c.get(t.Context(), "https://pds.test", f.fetch)
		is.NotError(t, err)
		is.True(t, !hit, "kept the cut-short fetch")
		is.Equal(t, 2, f.calls)
	})
}

func TestBlobRefusal(t *testing.T) {
	tests := []struct {
		status int
		name   string
		err    error
	}{
		{status: 401, name: "InvalidToken", err: model.ErrorPDSAuthFailed},
		{status: 403, name: "ScopeMissingError", err: model.ErrorBlobTypeRefused},
		{status: 403, name: "Forbidden", err: model.ErrorBlobRejected},
		{status: 400, name: "InvalidRequest", err: model.ErrorBlobRejected},
		{status: 413, name: "PayloadTooLarge", err: model.ErrorBlobTooLarge},
		{status: 429, name: "RateLimitExceeded", err: model.ErrorPDSUnavailable},
		{status: 500, name: "InternalServerError", err: model.ErrorPDSUnavailable},
		{status: 502, name: "", err: model.ErrorPDSUnavailable},
	}
	for _, test := range tests {
		t.Run(fmt.Sprint("should classify ", test.status, " ", test.name, " as ", test.err), func(t *testing.T) {
			is.Equal(t, test.err, blobRefusal(test.status, test.name))
		})
	}
}

func TestRefreshRefused(t *testing.T) {
	tests := []struct {
		err     error
		refused bool
	}{
		{err: errors.New("failed to refresh OAuth tokens: token refresh failed (HTTP 400): invalid_grant"), refused: true},
		{err: errors.New("failed to refresh OAuth tokens: token refresh failed: auth server request failed (HTTP 400): invalid_grant"), refused: true},
		{err: errors.New("failed to refresh OAuth tokens: token refresh failed (HTTP 503): unavailable"), refused: false},
		{err: errors.New("failed to refresh OAuth tokens: token refresh failed: dial tcp: connection refused"), refused: false},
		{err: errors.New("something else (HTTP 400)"), refused: false},
	}
	for _, test := range tests {
		t.Run(fmt.Sprint("should tell whether ", test.err, " is a refusal"), func(t *testing.T) {
			is.Equal(t, test.refused, refreshRefused(test.err))
		})
	}
}

// fetcher of a fixed description or error, counting its calls.
type fetcher struct {
	description model.ServerDescription
	err         error
	calls       int
}

func (f *fetcher) fetch(ctx context.Context, host string) (model.ServerDescription, error) {
	f.calls++
	if f.err != nil {
		return model.ServerDescription{}, f.err
	}
	return f.description, nil
}

func TestBlobCheck(t *testing.T) {
	t.Run("should pass the content's own CID, in any multibase, with the size sent", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			content := hegel.Draw(ht, hegel.Binary(0, 1024))
			contentCID := rawCID(content)
			encoding := hegel.Draw(ht, hegel.SampledFrom(multibases()))
			answered, err := contentCID.StringOfBase(encoding)
			is.NotError(ht, err)
			mimeType := hegel.Draw(ht, hegel.Text())

			blob := model.Blob{CID: model.CID(answered), MIMEType: mimeType, Size: int64(len(content))}
			is.Equal(ht, "", blobCheck(blob, contentCID, int64(len(content))))
		}, hegel.WithTestCases(2000))
	})

	t.Run("should fail a blob that differs from the content in any way, naming the first check that fails", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			content := hegel.Draw(ht, hegel.Binary(0, 1024))
			size := int64(len(content))
			contentCID := rawCID(content)
			answered, m := drawCIDAnswer(ht, contentCID)
			answeredSize := size
			if hegel.Draw(ht, hegel.WeightedBooleans(0.3)) {
				answeredSize = hegel.Draw(ht, hegel.Integers[int64](math.MinInt64, math.MaxInt64))
				ht.Assume(answeredSize != size)
			}
			ht.Assume(m.any() || answeredSize != size)

			want := "cid"
			switch {
			case answeredSize != size:
				want = "size"
			case m.syntax:
				want = "cid_syntax"
			case m.codec:
				want = "cid_codec"
			case m.hash:
				want = "cid_hash"
			}
			blob := model.Blob{CID: model.CID(answered), MIMEType: "audio/mpeg", Size: answeredSize}
			is.Equal(ht, want, blobCheck(blob, contentCID, size))
		}, hegel.WithTestCases(5000))
	})
}

func TestSameCID(t *testing.T) {
	t.Run("should take the record's own CID in any multibase", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			want := hegel.Draw(ht, recordCIDs())
			encoding := hegel.Draw(ht, hegel.SampledFrom(multibases()))
			answered, err := want.StringOfBase(encoding)
			is.NotError(ht, err)

			is.True(ht, sameCID(answered, want), "refused", answered)
		}, hegel.WithTestCases(2000))
	})

	t.Run("should refuse any other CID, and anything that is not a CID", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			want := hegel.Draw(ht, recordCIDs())
			answered, m := drawCIDAnswer(ht, want)
			ht.Assume(m.any())

			is.True(ht, !sameCID(answered, want), "took", answered)
		}, hegel.WithTestCases(2000))
	})
}

func TestRecordCID(t *testing.T) {
	t.Run("should give a dag-cbor CIDv1 with a SHA-256 multihash, whatever the key order and spacing of the JSON", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			record := hegel.Draw(ht, trackRecords())
			sorted, err := jsonv1.Marshal(record)
			is.NotError(ht, err)
			shuffled, err := json.Marshal(record, json.Deterministic(false), jsontext.Multiline(true))
			is.NotError(ht, err)

			c, err := recordCID(sorted)
			is.NotError(ht, err)
			is.Equal(ht, uint64(1), c.Version())
			is.Equal(ht, uint64(cid.DagCBOR), c.Prefix().Codec)
			is.Equal(ht, uint64(multihash.SHA2_256), c.Prefix().MhType)
			again, err := recordCID(shuffled)
			is.NotError(ht, err)
			is.True(ht, c.Equals(again), "CID depends on the JSON's form:", c, again)
		}, hegel.WithTestCases(1000))
	})

	t.Run("should give records with different titles different CIDs", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			record := hegel.Draw(ht, trackRecords())
			other := maps.Clone(record)
			other["title"] = hegel.Draw(ht, hegel.Text().MinSize(1))
			ht.Assume(other["title"] != record["title"])

			a, err := json.Marshal(record)
			is.NotError(ht, err)
			b, err := json.Marshal(other)
			is.NotError(ht, err)
			c1, err := recordCID(a)
			is.NotError(ht, err)
			c2, err := recordCID(b)
			is.NotError(ht, err)
			is.True(ht, !c1.Equals(c2), "same CID for different titles")
		}, hegel.WithTestCases(100))
	})
}

func TestRecordURIMismatch(t *testing.T) {
	t.Run("should find nothing wrong with a URI of the account's record in the collection with a TID key", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			did := hegel.Draw(ht, dids())
			collection := hegel.Draw(ht, nsids())
			rkey := hegel.Draw(ht, tids())

			uri, err := syntax.ParseATURI("at://" + did.String() + "/" + collection.String() + "/" + rkey)
			is.NotError(ht, err)
			is.Equal(ht, "", recordURIMismatch(uri, did, collection))
		}, hegel.WithTestCases(2000))
	})

	t.Run("should find a URI that differs in any part, naming the first part that does", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			did := hegel.Draw(ht, dids())
			collection := hegel.Draw(ht, nsids())

			authority, want := did.String(), ""
			answeredCollection := collection.String()
			rkey := hegel.Draw(ht, tids())
			var mismatches []string
			if hegel.Draw(ht, hegel.WeightedBooleans(0.4)) {
				if hegel.Draw(ht, hegel.Booleans()) {
					authority = hegel.Draw(ht, handles())
					mismatches = append(mismatches, "authority is not a DID")
				} else {
					authority = hegel.Draw(ht, dids()).String()
					ht.Assume(authority != did.String())
					mismatches = append(mismatches, "authority is another account's DID")
				}
			}
			if hegel.Draw(ht, hegel.WeightedBooleans(0.4)) {
				answeredCollection = hegel.Draw(ht, nsids()).String()
				ht.Assume(answeredCollection != collection.String())
				mismatches = append(mismatches, "collection is not the one asked for")
			}
			if hegel.Draw(ht, hegel.WeightedBooleans(0.4)) {
				rkey = hegel.Draw(ht, nonTIDRecordKeys(rkey))
				mismatches = append(mismatches, "record key is not a TID")
			}
			ht.Assume(len(mismatches) > 0)
			want = mismatches[0]

			raw := "at://" + authority + "/" + answeredCollection
			if rkey != "" {
				raw += "/" + rkey
			}
			uri, err := syntax.ParseATURI(raw)
			is.NotError(ht, err)
			is.Equal(ht, want, recordURIMismatch(uri, did, collection))
		}, hegel.WithTestCases(2000))
	})
}

func TestBounded(t *testing.T) {
	t.Run("should keep text of up to 256 bytes, and cut longer text to a prefix of whole characters, marked", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			s := hegel.Draw(ht, hegel.OneOf(
				hegel.Text().MaxSize(400),
				hegel.Map(hegel.Binary(0, 400), func(b []byte) string { return string(b) }),
				hegel.Map(hegel.Integers(0, 300), func(n int) string { return strings.Repeat("é", n) }),
				hegel.Map(hegel.Integers(0, 100), func(n int) string { return strings.Repeat("👨‍👩‍👧‍👦", n) }),
				// Continuation bytes, with no character boundary to cut at.
				hegel.Map(hegel.Integers(0, 300), func(n int) string { return "a" + strings.Repeat("\x80", n) }),
			))

			out := bounded(s)
			if len(s) <= 256 {
				is.Equal(ht, s, out)
				return
			}
			prefix, ok := strings.CutSuffix(out, "…[truncated]")
			is.True(ht, ok, "no marker on", out)
			is.True(ht, strings.HasPrefix(s, prefix), "not a prefix")
			is.True(ht, len(prefix) <= 256 && len(prefix) > 256-utf8.UTFMax, "prefix of", len(prefix), "bytes")
			if utf8.ValidString(s) {
				is.True(ht, utf8.ValidString(out), "cut a character")
			}
		}, hegel.WithTestCases(2000))
	})
}

// cidMutation says how a CID answered differs from the one expected.
type cidMutation struct {
	syntax, codec, hash, digest bool
}

func (m cidMutation) any() bool {
	return m.syntax || m.codec || m.hash || m.digest
}

// drawCIDAnswer for the expected CID: the CID itself, or one changed in any combination of its codec,
// its hash function and its digest, or something that is not a CID at all, in any multibase.
func drawCIDAnswer(ht *hegel.T, want cid.Cid) (string, cidMutation) {
	var m cidMutation
	if hegel.Draw(ht, hegel.WeightedBooleans(0.15)) {
		m.syntax = true
		return hegel.Draw(ht, notCIDs(want)), m
	}

	decoded, err := multihash.Decode(want.Hash())
	is.NotError(ht, err)
	codec, hashCode, digest := want.Prefix().Codec, decoded.Code, decoded.Digest
	v0 := false

	if hegel.Draw(ht, hegel.WeightedBooleans(0.3)) {
		m.codec = true
		if hegel.Draw(ht, hegel.WeightedBooleans(0.2)) {
			v0 = true
		} else {
			codec = hegel.Draw(ht, hegel.Filter(hegel.Integers[uint64](0, math.MaxInt64), func(c uint64) bool { return c != want.Prefix().Codec }))
		}
	}
	if !v0 && hegel.Draw(ht, hegel.WeightedBooleans(0.3)) {
		m.hash = true
		hashCode = hegel.Draw(ht, hegel.Filter(hegel.Integers[uint64](0, math.MaxInt64), func(c uint64) bool { return c != multihash.SHA2_256 }))
		digest = hegel.Draw(ht, hegel.Binary(0, 64))
	}
	if hegel.Draw(ht, hegel.WeightedBooleans(0.3)) {
		m.digest = true
		digest = slices.Clone(digest)
		if !v0 && (len(digest) == 0 || hegel.Draw(ht, hegel.Booleans())) {
			digest = hegel.Draw(ht, hegel.Filter(hegel.Binary(0, 64), func(d []byte) bool { return len(d) != len(digest) }))
		} else {
			bit := hegel.Draw(ht, hegel.Integers(0, len(digest)*8-1))
			digest[bit/8] ^= 1 << (bit % 8)
		}
	}

	mh := must(multihash.Encode(digest, hashCode))
	if v0 {
		return cid.NewCidV0(mh).String(), m
	}
	encoded, err := cid.NewCidV1(codec, mh).StringOfBase(hegel.Draw(ht, hegel.SampledFrom(multibases())))
	is.NotError(ht, err)
	return encoded, m
}

// notCIDs: random text, and CIDs with bytes added, cut short, or a character changed, that do not decode.
func notCIDs(want cid.Cid) hegel.Generator[string] {
	return hegel.Filter(hegel.OneOf(
		hegel.Text(),
		hegel.Map(hegel.Binary(1, 8), func(b []byte) string {
			s, _ := multibase.Encode(multibase.Base32, append(want.Bytes(), b...))
			return s
		}),
		hegel.Map(hegel.Integers(0, len(want.String())-1), func(n int) string { return want.String()[:n] }),
		// The version as a varint that is not minimal.
		hegel.Just(must(multibase.Encode(multibase.Base32, append([]byte{0x81, 0x00}, want.Bytes()[1:]...)))),
		hegel.Composite(func(tc hegel.TestCase) string {
			s := []byte(want.String())
			s[hegel.Draw(tc, hegel.Integers(0, len(s)-1))] = hegel.Draw(tc, hegel.Integers[byte](0, 255))
			return string(s)
		}),
	), func(s string) bool {
		_, err := cid.Decode(s)
		return err != nil
	})
}

// multibases a CID can be written in, but for the identity one that is not text.
func multibases() []multibase.Encoding {
	var encodings []multibase.Encoding
	for e := range multibase.EncodingToStr {
		if e != multibase.Identity {
			encodings = append(encodings, e)
		}
	}
	slices.Sort(encodings)
	return encodings
}

// rawCID of content, as a blob's.
func rawCID(content []byte) cid.Cid {
	return must(cid.NewPrefixV1(cid.Raw, multihash.SHA2_256).Sum(content))
}

// recordCIDs of track records.
func recordCIDs() hegel.Generator[cid.Cid] {
	return hegel.Map(trackRecords(), func(record map[string]any) cid.Cid {
		return must(recordCID(must(json.Marshal(record))))
	})
}

// trackRecords with any title and description, and any blob.
func trackRecords() hegel.Generator[map[string]any] {
	return hegel.Composite(func(tc hegel.TestCase) map[string]any {
		record := map[string]any{
			"$type": model.CollectionTrack.String(),
			"audio": map[string]any{"original": model.Blob{
				CID:      model.CID(rawCID(hegel.Draw(tc, hegel.Binary(0, 64))).String()),
				MIMEType: hegel.Draw(tc, hegel.FromRegex(`[a-z]{1,10}/[a-z0-9.+-]{1,20}`, true)),
				Size:     hegel.Draw(tc, hegel.Integers[int64](0, math.MaxInt64)),
			}},
			"title":     hegel.Draw(tc, hegel.Text().MinSize(1)),
			"createdAt": model.Time{T: hegel.Draw(tc, hegel.Datetimes())}.String(),
		}
		if hegel.Draw(tc, hegel.Booleans()) {
			record["description"] = hegel.Draw(tc, hegel.Text())
		}
		return record
	})
}

// dids of the plc and web methods, and of any other.
func dids() hegel.Generator[model.DID] {
	return hegel.Map(hegel.Filter(hegel.OneOf(
		hegel.FromRegex(`did:plc:[a-z2-7]{24}`, true),
		hegel.Map(hegel.Domains(), func(d string) string { return "did:web:" + d }),
		hegel.FromRegex(`did:[a-z]{1,8}:[a-zA-Z0-9._:%-]{0,40}[a-zA-Z0-9._-]`, true),
	), func(s string) bool {
		_, err := syntax.ParseDID(s)
		return err == nil
	}), func(s string) model.DID { return model.DID(s) })
}

// handles, which are no DIDs.
func handles() hegel.Generator[string] {
	return hegel.Filter(hegel.Domains(), func(s string) bool {
		_, err := syntax.ParseHandle(s)
		return err == nil
	})
}

// nsids of collections.
func nsids() hegel.Generator[model.NSID] {
	return hegel.Map(hegel.Filter(
		hegel.FromRegex(`[a-z][a-z0-9-]{0,10}(\.[a-z][a-z0-9-]{0,10}){1,3}\.[a-zA-Z][a-zA-Z0-9]{0,15}`, true),
		func(s string) bool {
			_, err := syntax.ParseNSID(s)
			return err == nil
		}), func(s string) model.NSID { return model.NSID(s) })
}

// tids from any time and clock.
func tids() hegel.Generator[string] {
	return hegel.Composite(func(tc hegel.TestCase) string {
		micros := hegel.Draw(tc, hegel.Integers[int64](0, 1<<53-1))
		clock := hegel.Draw(tc, hegel.Integers[uint](0, 1023))
		return syntax.NewTID(micros, clock).String()
	})
}

// nonTIDRecordKeys: none at all, self, the TID upper-cased, or any other record key that is no TID.
func nonTIDRecordKeys(tid string) hegel.Generator[string] {
	return hegel.Filter(hegel.OneOf(
		hegel.Just(""),
		hegel.Just("self"),
		hegel.Just(strings.ToUpper(tid)),
		hegel.Just(tid+"a"),
		hegel.FromRegex(`[a-zA-Z0-9_~.:-]{1,20}`, true),
	), func(s string) bool {
		if _, err := syntax.ParseTID(s); err == nil {
			return false
		}
		if s == "" {
			return true
		}
		_, err := syntax.ParseRecordKey(s)
		return err == nil
	})
}
