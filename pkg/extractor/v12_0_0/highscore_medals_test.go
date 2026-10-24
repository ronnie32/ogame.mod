package v12_0_0

import (
	"fmt"
	"os"
	"testing"
)

func TestExtractHighscoreExistingMedals(t *testing.T) {
	page, err := os.ReadFile("../../../samples/v12.0.27/en/highscore.html")
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewExtractor().ExtractHighscore(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Players) < 3 {
		t.Fatalf("players = %d, want at least 3", len(got.Players))
	}
	for i, want := range []int64{1, 2, 3} {
		if got.Players[i].Position != want {
			t.Errorf("medal position = %d, want %d", got.Players[i].Position, want)
		}
	}
}

func TestExtractHighscoreMedalPositions(t *testing.T) {
	// The first rank is ignored and has no homeworld link. Rank 2 must
	// remain 2 after that row is omitted from the returned players.
	row := func(id int, position, name string, coordinate bool) string {
		link := ""
		if coordinate {
			link = `<a href="?galaxy=1&amp;system=2&amp;position=3">home</a>`
		}
		return fmt.Sprintf(`<tr id="position%d"><td class="position">%s</td><td class="name">%s<span class="playername">%s</span></td><td class="score">100</td></tr>`, id, position, link, name)
	}
	page := `<div id="stat_list_content"><script>var site = 1; var currentCategory = 1; var currentType = 0;</script><table id="ranks"><tbody>` +
		row(100, `<div class="highscorePositionIcon position1"></div>`, "ignored", false) +
		row(101, `<div class="highscorePositionIcon position2"></div>`, "second", true) +
		row(102, `<div class="position3 highscorePositionIcon extra"></div>`, "third", true) +
		row(103, `4`, "fourth", true) + `</tbody></table></div>`
	got, err := NewExtractor().ExtractHighscore([]byte(page))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Players) != 3 {
		t.Fatalf("players = %d, want 3", len(got.Players))
	}
	for i, want := range []int64{2, 3, 4} {
		if got.Players[i].Position != want {
			t.Errorf("player %d position = %d, want %d", i, got.Players[i].Position, want)
		}
	}
}
