package launcher

import "testing"

func TestPlainTextTemplate(t *testing.T) {
	got := PlainText(Doc{
		Title:   "Notulensi Meeting - Kamis, 1 Oktober 2026",
		Content: "Notulensi Meeting - Kamis, 1 Oktober 2026\n\n## Waktu & Tempat\n- Waktu: \n- \n\n## Peserta\n-\n\n**Catatan**",
	})
	want := "Notulensi Meeting - Kamis, 1 Oktober 2026\n\nWAKTU & TEMPAT\n• Waktu:\n• \n\nPESERTA\n• \n\nCatatan\n"
	if got != want {
		t.Fatalf("PlainText:\n%q\nwant\n%q", got, want)
	}
}
