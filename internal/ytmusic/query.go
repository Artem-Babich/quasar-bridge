package ytmusic

import (
	"strings"
	"unicode"
)

// Request is the panel command for a music phrase.
type Request struct {
	Action string
	Query  string
}

// The route classifier has already decided that the phrase asks for music.
// Here only the command words are removed, so the rest is what to search.
var (
	leading = []string{"алиса", "джарвис", "пожалуйста", "включи", "включите", "поставь", "поставьте", "запусти", "сыграй", "играй", "давай", "послушаем", "хочу", "послушать", "мне", "нам", "пожалуйста"}
	filler  = map[string]bool{"песню": true, "песня": true, "песни": true, "трек": true, "треки": true, "музыку": true, "музыка": true, "группу": true, "группы": true, "исполнителя": true, "композицию": true}
	trailer = []string{"на ютубе", "в ютубе", "на youtube", "в youtube music", "в ютуб музыке", "пожалуйста"}
	likes   = map[string]bool{"лайки": true, "мои лайки": true, "понравившиеся": true, "мои понравившиеся": true, "любимые": true, "мои любимые": true, "любимые песни": true}
	vague   = map[string]bool{"": true, "что-нибудь": true, "что нибудь": true, "что-то": true, "чего-нибудь": true, "рекомендации": true, "мою волну": true}
)

// Parse turns a music phrase into a panel command.
func Parse(text string) Request {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == '!' || r == '?' || r == '.' || r == '«' || r == '»' || r == '"'
	})
	original := strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == '!' || r == '?' || r == '.' || r == '«' || r == '»' || r == '"'
	})
	start := 0
	for start < len(words) && contains(leading, words[start]) {
		start++
	}
	end := len(words)
	for {
		joined := strings.Join(words[start:end], " ")
		trimmed := false
		for _, t := range trailer {
			if strings.HasSuffix(joined, " "+t) || joined == t {
				end -= len(strings.Fields(t))
				trimmed = true
				break
			}
		}
		if !trimmed {
			break
		}
	}
	var kept []string
	var keptLower []string
	for i := start; i < end; i++ {
		if filler[words[i]] && i == start+len(kept) {
			// Only command fillers before the title: «песню Кукушка».
			continue
		}
		kept = append(kept, original[i])
		keptLower = append(keptLower, words[i])
	}
	lower := strings.Join(keptLower, " ")
	switch {
	case likes[lower]:
		return Request{Action: "play", Query: "мои лайки"}
	case vague[lower]:
		return Request{Action: "recommendations"}
	}
	return Request{Action: "play", Query: strings.Join(kept, " ")}
}

func contains(list []string, word string) bool {
	for _, w := range list {
		if w == word {
			return true
		}
	}
	return false
}
