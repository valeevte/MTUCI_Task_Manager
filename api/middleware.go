package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
)

// contextKey — тип для ключей контекста (чтобы не было коллизий)
type contextKey string

const userContextKey contextKey = "user"

// TelegramUser — данные пользователя из Telegram initData
type TelegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// withAuth — middleware, проверяющий авторизацию через Telegram initData
//
// Заголовок запроса должен содержать: Authorization: tma <initData>
// initData — строка, которую Telegram передаёт в WebApp
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// ============================================================
		// DEV_MODE — режим разработки (пропускаем проверку авторизации)
		// Позволяет тестировать API в браузере без Telegram
		// ============================================================
		if os.Getenv("DEV_MODE") == "true" {
			log.Println("⚠️  DEV_MODE: авторизация пропущена")
			user := &TelegramUser{ID: 12345, FirstName: "Developer"}
			ctx := context.WithValue(r.Context(), userContextKey, user)
			next(w, r.WithContext(ctx))
			return
		}

		// Получаем заголовок Authorization
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "отсутствует заголовок Authorization",
			})
			return
		}

		// Извлекаем initData (убираем префикс "tma ")
		initData := strings.TrimPrefix(authHeader, "tma ")

		// Валидируем подпись и извлекаем данные пользователя
		user, err := validateInitData(initData, s.botToken)
		if err != nil {
			log.Printf("❌ Ошибка авторизации: %v", err)
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "неверная авторизация: " + err.Error(),
			})
			return
		}

		// Сохраняем пользователя в контекст запроса
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next(w, r.WithContext(ctx))
	}
}

// ============================================================
// validateInitData проверяет подпись initData от Telegram
//
// Алгоритм (из документации Telegram):
// 1. Парсим initData как URL query string
// 2. Извлекаем hash, остальные параметры сортируем по алфавиту
// 3. Формируем data_check_string: "key=value\nkey=value\n..."
// 4. secret_key = HMAC-SHA256(key="WebAppData", data=bot_token)
// 5. Вычисляем HMAC-SHA256(key=secret_key, data=data_check_string)
// 6. Сравниваем с hash из initData
//
// Документация: https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app
// ============================================================
func validateInitData(initData, botToken string) (*TelegramUser, error) {
	// Парсим initData как URL query string
	values, err := url.ParseQuery(initData)
	if err != nil {
		return nil, fmt.Errorf("неверный формат initData")
	}

	// Извлекаем hash (подпись от Telegram)
	hash := values.Get("hash")
	if hash == "" {
		return nil, fmt.Errorf("hash не найден в initData")
	}

	// Убираем hash из параметров (он не участвует в проверке)
	values.Del("hash")

	// Сортируем оставшиеся ключи по алфавиту
	var keys []string
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Формируем data_check_string: "key=value\nkey=value"
	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, values.Get(k)))
	}
	dataCheckString := strings.Join(pairs, "\n")

	// Шаг 1: secret_key = HMAC-SHA256(key="WebAppData", data=botToken)
	mac := hmac.New(sha256.New, []byte("WebAppData"))
	mac.Write([]byte(botToken))
	secretKey := mac.Sum(nil)

	// Шаг 2: hash = HMAC-SHA256(key=secretKey, data=dataCheckString)
	mac2 := hmac.New(sha256.New, secretKey)
	mac2.Write([]byte(dataCheckString))
	calculatedHash := hex.EncodeToString(mac2.Sum(nil))

	// Сравниваем вычисленный hash с полученным
	if calculatedHash != hash {
		return nil, fmt.Errorf("подпись не совпадает")
	}

	// Извлекаем данные пользователя из параметра "user"
	userData := values.Get("user")
	if userData == "" {
		return nil, fmt.Errorf("данные пользователя не найдены")
	}

	var user TelegramUser
	if err := json.Unmarshal([]byte(userData), &user); err != nil {
		return nil, fmt.Errorf("ошибка парсинга данных пользователя: %v", err)
	}

	return &user, nil
}
