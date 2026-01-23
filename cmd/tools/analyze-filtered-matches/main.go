package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <log_file>")
		fmt.Println("Or pipe logs: docker logs vodeneevbet-parser 2>&1 | go run main.go -")
		os.Exit(1)
	}

	var scanner *bufio.Scanner
	if os.Args[1] == "-" {
		scanner = bufio.NewScanner(os.Stdin)
	} else {
		file, err := os.Open(os.Args[1])
		if err != nil {
			fmt.Printf("Error opening file: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()
		scanner = bufio.NewScanner(file)
	}

	// Regex для парсинга логов фильтрации
	filterRegex := regexp.MustCompile(`filtered by isValidMatch.*Team1="([^"]*)".*Team2="([^"]*)".*Name="([^"]*)"`)

	// Категории фильтрации
	categories := make(map[string][]FilteredMatch)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "filtered by isValidMatch") {
			continue
		}

		matches := filterRegex.FindStringSubmatch(line)
		if len(matches) < 4 {
			continue
		}

		team1 := matches[1]
		team2 := matches[2]
		name := matches[3]

		reason := categorizeFilterReason(team1, team2, name)
		categories[reason] = append(categories[reason], FilteredMatch{
			Team1: team1,
			Team2: team2,
			Name:  name,
		})
	}

	// Выводим результаты по категориям
	fmt.Println("=== Анализ отфильтрованных матчей ===")
	fmt.Println()

	total := 0
	for reason, matches := range categories {
		total += len(matches)
		fmt.Printf("📋 Категория: %s\n", reason)
		fmt.Printf("   Количество: %d\n", len(matches))
		fmt.Printf("   Примеры:\n")
		
		// Показываем до 5 примеров
		maxExamples := 5
		if len(matches) < maxExamples {
			maxExamples = len(matches)
		}
		
		for i := 0; i < maxExamples; i++ {
			m := matches[i]
			fmt.Printf("     - %s vs %s", m.Team1, m.Team2)
			if m.Name != "" {
				fmt.Printf(" (Name: %q)", m.Name)
			}
			fmt.Println()
		}
		if len(matches) > maxExamples {
			fmt.Printf("     ... и еще %d примеров\n", len(matches)-maxExamples)
		}
		fmt.Println()
	}

	fmt.Printf("Всего отфильтровано: %d матчей\n", total)
}

type FilteredMatch struct {
	Team1 string
	Team2 string
	Name  string
}

func categorizeFilterReason(team1, team2, name string) string {
	// Пустые команды
	if team1 == "" && team2 == "" {
		if name != "" {
			return "Обе команды пустые (специальное событие/ставка)"
		}
		return "Обе команды пустые"
	}
	if team1 == "" {
		return "Team1 пустая"
	}
	if team2 == "" {
		return "Team2 пустая"
	}

	// Одинаковые команды
	if team1 == team2 {
		return "Одинаковые команды"
	}

	// Короткие названия
	if len(team1) < 2 || len(team2) < 2 {
		return "Слишком короткое название команды"
	}

	// Общие названия
	genericTeams := []string{"Хозяева", "Гости", "Home", "Away", "Team 1", "Team 2", "TBD", "vs"}
	for _, gt := range genericTeams {
		if team1 == gt || team2 == gt {
			return fmt.Sprintf("Общее название команды: %q", gt)
		}
	}

	// Короткое имя матча
	if name != "" && len(name) < 5 {
		return fmt.Sprintf("Слишком короткое имя матча (%d символов)", len(name))
	}

	return "Неизвестная причина"
}
