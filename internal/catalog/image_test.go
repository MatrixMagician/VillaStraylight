package catalog

import (
	"regexp"
	"testing"
)

// TestImageResolvesBothRows: the two compiled-in image entries resolve by id to
// their measured weight footprint and their diffusion file, and an unknown id is a
// miss rather than a zero entry a caller might size or render.
func TestImageResolvesBothRows(t *testing.T) {
	q8, ok := Image("z-image-turbo")
	if !ok {
		t.Fatal("z-image-turbo is not in the image table")
	}
	if q8.Diffusion.Filename != "z_image_turbo-Q8_0.gguf" || q8.WeightBytes != 9236591616 || q8.ComputeBytes != 1258291200 {
		t.Errorf("z-image-turbo = %s / %d / %d, want z_image_turbo-Q8_0.gguf / 9236591616 / 1258291200", q8.Diffusion.Filename, q8.WeightBytes, q8.ComputeBytes)
	}
	q4, ok := Image("z-image-turbo-q4")
	if !ok {
		t.Fatal("z-image-turbo-q4 is not in the image table")
	}
	if q4.Diffusion.Filename != "z_image_turbo-Q4_K.gguf" || q4.WeightBytes != 6523315200 {
		t.Errorf("z-image-turbo-q4 = %s / %d, want z_image_turbo-Q4_K.gguf / 6523315200", q4.Diffusion.Filename, q4.WeightBytes)
	}
	if q8.TextEncoder != q4.TextEncoder || q8.VAE != q4.VAE {
		t.Error("the two quants share one text encoder and one VAE; the rows disagree")
	}
	if _, ok := Image("z-image-turbo-q2"); ok {
		t.Error("an unknown id resolved; a caller would size and render a model that does not exist")
	}
	if _, ok := Image(""); ok {
		t.Error("the empty id resolved")
	}
}

// TestImageShardsAreTheThreeFilesInFlagOrder: the pull manifest is the diffusion
// model, the text encoder, then the VAE, which is the order sd-server names them
// under --diffusion-model, --llm and --vae.
func TestImageShardsAreTheThreeFilesInFlagOrder(t *testing.T) {
	m, _ := Image("z-image-turbo")
	got := m.Shards()
	if len(got) != 3 {
		t.Fatalf("Shards() = %d files, want 3", len(got))
	}
	want := []string{"z_image_turbo-Q8_0.gguf", "Qwen3-4B-Instruct-2507-Q4_K_M.gguf", "z-image-turbo-ae.safetensors"}
	for i, sh := range got {
		if sh.Filename != want[i] {
			t.Errorf("Shards()[%d] = %q, want %q", i, sh.Filename, want[i])
		}
	}
}

// TestImageTableIsWellFormed guards every row at build time: a mistyped row must
// fail here, not at sd-server's argv or in a reservation that reserves nothing.
// Steps, cfg scale and the dimensions are held to the ranges sd-server accepts and
// the measurement covered; every shard carries a resolvable URL, a filename, a
// 64-hex SHA-256 and a size; the footprint and the provenance are non-zero.
func TestImageTableIsWellFormed(t *testing.T) {
	sha := regexp.MustCompile(`^[0-9a-f]{64}$`)
	seen := map[string]bool{}
	for _, m := range imageModels {
		t.Run(m.ID, func(t *testing.T) {
			if m.ID == "" || seen[m.ID] {
				t.Errorf("id %q is empty or duplicated", m.ID)
			}
			seen[m.ID] = true
			if m.DisplayName == "" || m.Quant == "" || m.Provenance == "" {
				t.Error("display_name, quant and provenance are each required")
			}
			if m.WeightBytes == 0 || m.ComputeBytes == 0 {
				t.Errorf("weight_bytes %d / compute_bytes %d: the reservation would reserve nothing", m.WeightBytes, m.ComputeBytes)
			}
			if m.Steps < 1 || m.Steps > 64 {
				t.Errorf("steps %d out of range [1, 64]", m.Steps)
			}
			if m.CfgScale < 0 || m.CfgScale > 30 {
				t.Errorf("cfg_scale %g out of range [0, 30]", m.CfgScale)
			}
			for _, dim := range []int{m.Width, m.Height} {
				if dim < 256 || dim > 2048 || dim%64 != 0 {
					t.Errorf("dimension %d is not a multiple of 64 in [256, 2048]", dim)
				}
			}
			for _, sh := range m.Shards() {
				if sh.URL == "" || sh.Filename == "" || sh.SizeBytes == 0 || !sha.MatchString(sh.SHA256) {
					t.Errorf("shard %+v is missing a url, filename, size or 64-hex sha256", sh)
				}
			}
		})
	}
	if len(seen) != 2 {
		t.Errorf("the table holds %d rows, want the two measured quants", len(seen))
	}
}

// TestImageFilenamesAreDisjointFromTheChatCatalog: the image files share the models
// dir with every chat model and sidecar, so a filename collision would let one pull
// overwrite the other. The VAE is renamed from upstream's ae.safetensors for this
// reason, and the rule is asserted for every file.
func TestImageFilenamesAreDisjointFromTheChatCatalog(t *testing.T) {
	seed, err := decodeSeed()
	if err != nil {
		t.Fatalf("decode seed: %v", err)
	}
	chat := map[string]string{}
	for _, m := range seed.Models {
		for _, sh := range m.AllShards() {
			chat[sh.Filename] = m.ID
		}
	}
	image := map[string]string{}
	for _, m := range imageModels {
		for _, sh := range m.Shards() {
			if owner, clash := chat[sh.Filename]; clash {
				t.Errorf("image %s file %q is also chat entry %s's file", m.ID, sh.Filename, owner)
			}
			if owner, clash := image[sh.Filename]; clash && owner != m.ID && sh != sharedShard(owner, sh.Filename) {
				t.Errorf("image %s and %s both name %q with different contents", m.ID, owner, sh.Filename)
			}
			image[sh.Filename] = m.ID
		}
	}
}

// sharedShard returns the shard of the named image entry with the given filename,
// so two rows may share one file only when they agree on its URL, checksum and size.
func sharedShard(id, filename string) Shard {
	m, _ := Image(id)
	for _, sh := range m.Shards() {
		if sh.Filename == filename {
			return sh
		}
	}
	return Shard{}
}
