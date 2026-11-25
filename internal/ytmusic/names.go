package ytmusic

import (
	"context"
	"encoding/json/v2"
	"strings"
	"time"

	"github.com/skrashevich/quasar-bridge/internal/llm"
)

// namesTimeout bounds the model call: music must not wait long for a better
// search; after it the spoken words are searched as before.
const namesTimeout = 2500 * time.Millisecond

const namesPrompt = `Пользователь просит включить музыку. Назови исполнителя и песню из его фразы так, как они записаны в музыкальном каталоге: в именительном падеже и в оригинальном написании (иностранные исполнители — латиницей).
Слова «группа», «песня», «трек», «альбом», «исполнитель» в названия не входят.
Не додумывай: если исполнитель или песня не названы, оставь пустую строку.
Ответь только JSON: {"artist":"...","title":"..."}`

// Names is what the phrase asks to play, spelled as in the catalog.
type Names struct {
	Artist string `json:"artist"`
	Title  string `json:"title"`
}

// names asks the model for the artist and the song. ok is false when the
// model is missing, slow, fails or names nothing.
func (p *Player) names(ctx context.Context, text string) (Names, bool) {
	if p.Model == nil {
		return Names{}, false
	}
	timeout := p.NamesTimeout
	if timeout <= 0 {
		timeout = namesTimeout
	}
	ctx, cancel := context.WithTimeout(llm.WithoutHostedTools(ctx), timeout)
	defer cancel()
	answer, err := p.Model.Complete(ctx, []llm.Message{{Role: "system", Content: namesPrompt}, {Role: "user", Content: text}}, true)
	if err != nil {
		return Names{}, false
	}
	var n Names
	if json.Unmarshal([]byte(answer), &n) != nil {
		return Names{}, false
	}
	n.Artist, n.Title = strings.TrimSpace(n.Artist), strings.TrimSpace(n.Title)
	return n, n.Artist != "" || n.Title != ""
}
