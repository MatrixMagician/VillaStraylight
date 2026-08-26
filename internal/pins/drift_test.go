// drift_test.go turns a comment into a test.
//
// internal/orchestrate/memory.go documents embedImage as "a DELIBERATELY
// INDEPENDENT const" whose literal is "byte-identical to the kyuz0 Strix-Halo
// Vulkan RADV vulkanImage", and carried a bare `// == vulkanImage` line above the
// value. Nothing checked it. One image serves two roles — the inference backend
// and the embeddings server — so a contributor updating one and not the other
// would silently make the comment false, and nothing in the tree would notice.
//
// This is a live hole independent of `villa update`, which is why it is its own
// file rather than a clause inside the table's tests.
package pins

import "testing"

// TestEmbedderAndVulkanShareOneVettedPin asserts the equality the comment used to
// merely claim.
//
// VETTED PINS ONLY, and this is load-bearing rather than incidental. The effective
// pins are free to diverge the moment memory updates and the inference backend does
// not — that is a legitimate, expected state, and a test over effective pins would
// fire on correct behaviour. The claim being guarded is narrower and historical:
// "villa validated both roles against the same image", never "these must be equal
// at runtime".
func TestEmbedderAndVulkanShareOneVettedPin(t *testing.T) {
	embedder, ok := Lookup(Embedder)
	if !ok {
		t.Fatal("the table no longer names the embedder")
	}
	vulkan, ok := Lookup(BackendVulkan)
	if !ok {
		t.Fatal("the table no longer names the vulkan backend")
	}

	got, want := embedder.Vetted().Ref, vulkan.Vetted().Ref
	if got != want {
		t.Errorf(`the embedder and the vulkan backend no longer share one vetted pin:

  embedder: %s
  vulkan:   %s

These are two roles served by ONE image, and villa vetted both against the same
bytes. A mismatch means one of the two literals was advanced and the other was
not, which is almost always an oversight — the embedder would then be running an
image nobody proved in that role, or the backend one nobody proved in its.

If the divergence is DELIBERATE (upstream split the image, or the two roles now
genuinely want different builds), delete this test. That is a valid and expected
end for it: a deleted test is a visible act in a diff, which the comment this
replaced never was.`, got, want)
	}
}
