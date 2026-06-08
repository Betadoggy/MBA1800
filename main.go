package main

import (
	"database/sql"
	"encoding/json"
	"html/template"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq"
)

type Problem struct {
	ID             int               `json:"id"`
	OriginalNumber string            `json:"original_number"`
	Category       string            `json:"category"`
	Question       string            `json:"question"`
	BoxContent     string            `json:"box_content"`
	TableContent   string            `json:"table_content"`
	ImageURL       string            `json:"image_url"`
	HasImage       bool              `json:"has_image"`
	Options        map[string]string `json:"options"`
	Answer         int               `json:"answer"`
	Explanation    string            `json:"explanation"`
}

type RenderData struct {
	Problem
	TableContent template.HTML
	IsBookmarked bool // 즐겨찾기 여부 추가
}

type SubmitRequest struct {
	UserID         int `json:"user_id"`
	ProblemID      int `json:"problem_id"`
	SelectedOption int `json:"selected_option"`
	TimeSpent      int `json:"time_spent"`
}

type BookmarkRequest struct {
	ProblemID int  `json:"problem_id"`
	Bookmark  bool `json:"bookmark"`
}

type Progress struct {
	ProblemID    int       `json:"problem_id"`
	IntervalDays int       `json:"interval_days"`
	EaseFactor   float64   `json:"ease_factor"`
	Repetitions  int       `json:"repetitions"`
	NextReview   time.Time `json:"next_review"`
	IsBookmarked bool      `json:"is_bookmarked"`
}

var globalProblems []Problem
var problemMap map[int]Problem
var db *sql.DB

func main() {
	rand.Seed(time.Now().UnixNano())

	fileBytes, err := os.ReadFile("data.json")
	if err != nil {
		println("data.json 로드 실패.")
		globalProblems = []Problem{}
	} else {
		json.Unmarshal(fileBytes, &globalProblems)
	}

	problemMap = make(map[int]Problem)
	for _, p := range globalProblems {
		problemMap[p.ID] = p
	}
	println("성공적으로 " + strconv.Itoa(len(globalProblems)) + "개의 문제를 로드했습니다.")

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgresql://postgres.slqdijguwqccgdkbillw:Mgs%5E98092222@aws-1-ap-northeast-2.pooler.supabase.com:6543/postgres"
	}

	var dbErr error
	db, dbErr = sql.Open("postgres", dbURL)
	if dbErr != nil {
		panic(dbErr)
	}
	defer db.Close()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// 3. 메인 화면 서빙 (현재 문제의 즐겨찾기 상태를 DB에서 함께 조회)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if len(globalProblems) == 0 {
			http.Error(w, "문제가 없습니다.", http.StatusInternalServerError)
			return
		}

		var targetProblem Problem
		idStr := r.URL.Query().Get("id")

		if idStr != "" {
			targetID, err := strconv.Atoi(idStr)
			if err == nil {
				if p, exists := problemMap[targetID]; exists {
					targetProblem = p
				}
			}
		}

		if targetProblem.ID == 0 {
			targetProblem = globalProblems[rand.Intn(len(globalProblems))]
		}

		// DB에서 이 문제의 즐겨찾기 상태 확인
		isBookmarked := false
		db.QueryRow("SELECT is_bookmarked FROM user_progress WHERE problem_id = $1", targetProblem.ID).Scan(&isBookmarked)

		renderData := RenderData{
			Problem:      targetProblem,
			TableContent: template.HTML(targetProblem.TableContent),
			IsBookmarked: isBookmarked,
		}

		// [수정] 템플릿 파싱 에러 핸들링 추가
		tmpl, err := template.New("quiz").Parse(getHTMLSource())
		if err != nil {
			http.Error(w, "템플릿 에러: "+err.Error(), http.StatusInternalServerError)
			return
		}
		tmpl.Execute(w, renderData)
	})

	// 4. 채점 및 DB 진도 갱신 API
	http.HandleFunc("/api/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		var req SubmitRequest
		json.NewDecoder(r.Body).Decode(&req)

		targetProblem, next := problemMap[req.ProblemID]
		if !next {
			return
		}

		isCorrect := (req.SelectedOption == targetProblem.Answer)

		var p Progress
		err := db.QueryRow("SELECT problem_id, interval_days, ease_factor, repetitions, next_review, is_bookmarked FROM user_progress WHERE problem_id = $1", req.ProblemID).
			Scan(&p.ProblemID, &p.IntervalDays, &p.EaseFactor, &p.Repetitions, &p.NextReview, &p.IsBookmarked)

		if err == sql.ErrNoRows {
			p = Progress{ProblemID: req.ProblemID, EaseFactor: 2.5, IntervalDays: 0, Repetitions: 0, IsBookmarked: false}
		}

		if isCorrect {
			if p.Repetitions == 0 {
				p.IntervalDays = 1
			} else if p.Repetitions == 1 {
				p.IntervalDays = 3
			} else {
				p.IntervalDays = int(float64(p.IntervalDays) * p.EaseFactor)
			}
			p.Repetitions++
		} else {
			p.IntervalDays = 1
			p.Repetitions = 0
			p.EaseFactor = p.EaseFactor - 0.15
			if p.EaseFactor < 1.3 {
				p.EaseFactor = 1.3
			}
		}
		p.NextReview = time.Now().AddDate(0, 0, p.IntervalDays)

		query := `
			INSERT INTO user_progress (problem_id, interval_days, ease_factor, repetitions, next_review, is_bookmarked)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (problem_id) 
			DO UPDATE SET interval_days = $2, ease_factor = $3, repetitions = $4, next_review = $5;
		`
		_, err = db.Exec(query, p.ProblemID, p.IntervalDays, p.EaseFactor, p.Repetitions, p.NextReview, p.IsBookmarked)
		if err != nil {
			println("DB 저장 에러:", err.Error())
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"is_correct":  isCorrect,
			"answer":      targetProblem.Answer,
			"explanation": targetProblem.Explanation,
		})
	})

	// 5. [신규] 즐겨찾기 토글 API
	http.HandleFunc("/api/bookmark", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		var req BookmarkRequest
		json.NewDecoder(r.Body).Decode(&req)

		query := `
			INSERT INTO user_progress (problem_id, interval_days, ease_factor, repetitions, next_review, is_bookmarked)
			VALUES ($1, 0, 2.5, 0, NOW(), $2)
			ON CONFLICT (problem_id) 
			DO UPDATE SET is_bookmarked = $2;
		`
		_, err := db.Exec(query, req.ProblemID, req.Bookmark)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	println("DB 연동 서버 가동: http://localhost:" + port)
	http.ListenAndServe(":"+port, nil)
}

func getHTMLSource() string {
	return `
<!DOCTYPE html>
<html lang="ko">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>MBA 1800</title>
    <script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-white text-slate-900 max-w-md mx-auto p-4 font-sans">

    <div class="flex justify-between items-center text-xs text-slate-500 mb-4 border-b pb-2 gap-2">
        <div class="flex items-center gap-2">
            <span>{{.Category}} · {{.OriginalNumber}}</span>
            <button onclick="toggleBookmark()" id="bookmarkBtn" class="text-sm focus:outline-none transition-transform active:scale-125">
                {{if .IsBookmarked}}<span class="text-yellow-400">★</span>{{else}}<span class="text-slate-300">☆</span>{{end}}
            </button>
        </div>
        
        <form action="/" method="GET" class="flex items-center gap-1">
            <input type="number" name="id" placeholder="ID 입력" required
                   class="w-16 border rounded px-1.5 py-0.5 text-center text-slate-800 focus:outline-none focus:border-slate-400 [appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none">
            <button type="submit" class="bg-slate-100 hover:bg-slate-200 border text-slate-600 px-2 py-0.5 rounded font-medium">이동</button>
        </form>
    </div>

    <h2 class="text-base font-semibold leading-snug mb-4">{{.Question}}</h2>

    {{if .BoxContent}}
    <div class="bg-slate-50 border p-3 text-xs text-slate-600 whitespace-pre-wrap mb-4 font-mono">{{.BoxContent}}</div>
    {{end}}

    {{if .TableContent}}
    <div class="overflow-x-auto text-xs border rounded mb-4 p-1">
        {{.TableContent}}
    </div>
    {{end}}

    {{if .HasImage}}
    <img src="/{{.ImageURL}}" class="w-full h-auto border rounded mb-4">
    {{end}}

    <div class="space-y-2" id="options">
        {{range $key, $value := .Options}}
        <button onclick="submit('{{$key}}')" class="w-full text-left p-3 border rounded text-sm hover:bg-slate-50 active:bg-slate-100 flex gap-2">
            <span class="font-bold text-slate-400">{{$key}}.</span>
            <span>{{$value}}</span>
        </button>
        {{end}}
    </div>

    <div id="result" class="hidden mt-6 pt-4 border-t">
        <div id="status" class="text-base font-bold mb-2"></div>
        <div class="bg-slate-50 p-3 rounded text-xs text-slate-600 leading-relaxed whitespace-pre-wrap mb-4"><span class="block font-bold text-slate-400 mb-1">【 해설 】</span><span id="explanation"></span></div>
        <button onclick="nextProblem()" class="w-full py-2.5 bg-slate-900 text-white font-medium text-sm rounded">
            다음 문제
        </button>
    </div>

    <script>
        const start = Date.now();
        let submitted = false;
        let isBookmarked = {{.IsBookmarked}}; // 초기 상태 서버에서 바인딩

        // [신규] 별표 토글 함수
        function toggleBookmark() {
            const nextState = !isBookmarked;
            
            fetch('/api/bookmark', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    problem_id: {{.ID}},
                    bookmark: nextState
                })
            })
            .then(res => {
                if (res.ok) {
                    isBookmarked = nextState;
                    const btn = document.getElementById('bookmarkBtn');
                    btn.innerHTML = isBookmarked ? '<span class="text-yellow-400">★</span>' : '<span class="text-slate-300">☆</span>';
                }
            });
        }

        function submit(option) {
            if (submitted) return;
            submitted = true;

            const elapsed = Math.floor((Date.now() - start) / 1000);

            fetch('/api/submit', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    user_id: 1,
                    problem_id: {{.ID}},
                    selected_option: parseInt(option),
                    time_spent: elapsed
                })
            })
            .then(res => res.json())
            .then(data => {
                document.getElementById('options').classList.add('opacity-50');
                
                const status = document.getElementById('status');
                if (data.is_correct) {
                    status.innerText = "O 정답입니다.";
                    status.className = "text-sm font-bold text-blue-600 mb-2";
                } else {
                    status.innerText = "X 틀렸습니다. (정답: " + data.answer + "번)";
                    status.className = "text-sm font-bold text-red-600 mb-2";
                }

                document.getElementById('explanation').innerText = data.explanation.trim();
                document.getElementById('result').classList.remove('hidden');
                document.getElementById('result').scrollIntoView({ behavior: 'smooth' });
            });
        }

        function nextProblem() {
            location.href = "/";
        }
    </script>
</body>
</html>
`
}
