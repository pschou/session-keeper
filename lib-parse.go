package main

import (
	"log"
	"strconv"
	"strings"
)

func hypenRange(r string) (out map[int]struct{}) {
	out = make(map[int]struct{})
	for _, commaPt := range strings.Split(r, ",") {
		hyphenPts := strings.Split(commaPt, "-")
		switch len(hyphenPts) {
		case 1:
			if p, err := strconv.Atoi(hyphenPts[0]); err != nil {
				log.Fatal("Error parsing port:", err)
			} else {
				out[p] = struct{}{}
			}
		case 2:
			if st, err := strconv.Atoi(hyphenPts[0]); err != nil {
				log.Fatal("Error parsing range start:", err)
			} else if en, err := strconv.Atoi(hyphenPts[1]); err != nil {
				log.Fatal("Error parsing range end:", err)
			} else {
				for ; st <= en; st++ {
					out[st] = struct{}{}
				}
			}
		default:
			log.Fatal("Invalid range:", commaPt)
		}
	}
	return
}
