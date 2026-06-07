package main

import (
	"encoding/json"
	"html/template"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"
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
}

type SubmitRequest struct {
	UserID         int `json:"user_id"`
	ProblemID      int `json:"problem_id"`
	SelectedOption int `json:"selected_option"`
	TimeSpent      int `json:"time_spent"`
}

type Progress struct {
	ProblemID    int       `json:"problem_id"`
	IntervalDays int       `json:"interval_days"`
	EaseFactor   float64   `json:"ease_factor"`
	Repetitions  int       `json:"repetitions"`
	NextReview   time.Time `json:"next_review"`
}

var globalProblems []Problem
var problemMap map[int]Problem
var userProgress map[int]Progress

func main() {
	// 1. [가장 먼저 실행] 실행할 때마다 완전한 무작위 시드 강제 부여
	rand.Seed(time.Now().UnixNano())

	// 2. 원본 문제 데이터 로드 (data.json)
	fileBytes, err := os.ReadFile("data.json")
	if err != nil {
		println("data.json 로드 실패. 빈 데이터로 시작합니다.")
		globalProblems = []Problem{}
	} else {
		json.Unmarshal(fileBytes, &globalProblems)
	}

	problemMap = make(map[int]Problem)
	for _, p := range globalProblems {
		problemMap[p.ID] = p
	}
	println("성공적으로 " + strconv.Itoa(len(globalProblems)) + "개의 문제를 로드했습니다.")

	// 3. 진도 데이터 로드
	userProgress = make(map[int]Progress)
	progressBytes, err := os.ReadFile("user_progress.json")
	if err == nil {
		json.Unmarshal(progressBytes, &userProgress)
		println("기존 유저 진도 데이터 로드 완료 (" + strconv.Itoa(len(userProgress)) + "개)")
	}

	// 3. 메인 화면 서빙 (일반 접속: 랜덤 / 쿼리 파라미터 ?id=번호 가 있으면 해당 문제)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if len(globalProblems) == 0 {
			http.Error(w, "문제가 없습니다.", http.StatusInternalServerError)
			return
		}

		var targetProblem Problem
		idStr := r.URL.Query().Get("id")

		if idStr != "" {
			// URL에 ?id=104 처럼 숫자가 넘어온 경우 해당 문제 탐색
			targetID, err := strconv.Atoi(idStr)
			if err == nil {
				if p, exists := problemMap[targetID]; exists {
					targetProblem = p
				}
			}
		}

		// 만약 유효한 ID가 지정되지 않았거나 못 찾았다면 기존처럼 랜덤 추출
		if targetProblem.ID == 0 {
			targetProblem = globalProblems[rand.Intn(len(globalProblems))]
		}

		renderData := RenderData{
			Problem:      targetProblem,
			TableContent: template.HTML(targetProblem.TableContent),
		}

		tmpl, _ := template.New("quiz").Parse(getHTMLSource())
		tmpl.Execute(w, renderData)
	})

	// 4. 채점 및 Anki 진도 계산 API
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

		p, exists := userProgress[req.ProblemID]
		if !exists {
			p = Progress{ProblemID: req.ProblemID, EaseFactor: 2.5}
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
		userProgress[req.ProblemID] = p

		progressJSON, _ := json.MarshalIndent(userProgress, "", "  ")
		os.WriteFile("user_progress.json", progressJSON, 0644)

		println("["+time.Now().Format("01-02 15:04:05")+"]", "문제:", req.ProblemID, "| 결과:", isCorrect, "| 주:", p.IntervalDays)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"is_correct":  isCorrect,
			"answer":      targetProblem.Answer,
			"explanation": targetProblem.Explanation,
		})
	})

	println("미니멀 퀴즈 서버 가동: http://localhost:8080")
	http.ListenAndServe(":8080", nil)
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

    <div class="flex justify-between items-center text-xs text-slate-500 mb-4 border-b pb-2">
        <div>{{.Category}} · {{.OriginalNumber}}</div>
        
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
        <button onclick="location.reload()" class="w-full py-2.5 bg-slate-900 text-white font-medium text-sm rounded">
            다음 문제
        </button>
    </div>

    <script>
        const start = Date.now();
        let submitted = false;

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

                // 공백 찌꺼기 추적용 정규식을 걷어내고, 무결한 태그에 단순 .trim()으로 깔끔하게 매핑합니다.
                document.getElementById('explanation').innerText = data.explanation.trim();
                
                document.getElementById('result').classList.remove('hidden');
                document.getElementById('result').scrollIntoView({ behavior: 'smooth' });
            });
        }
    </script>
</body>
</html>
`
}
