package main

import (
    "math/rand"
    "time"
)

func RandomString(length int) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
    chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    result := make([]byte, length)
    for i := 0; i < length; i++ {
        result[i] = chars[r.Intn(len(chars))]
    }
    return string(result)
}