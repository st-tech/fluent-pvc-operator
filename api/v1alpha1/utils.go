package v1alpha1

import "math/rand"

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}
