package robotics

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Kilograms normalizes explicitly identified mass units. It never guesses a unit.
func Kilograms(value float64, unit string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "kg", "kilogram", "kilograms":
		return value, nil
	case "g", "gram", "grams":
		return value / 1000, nil
	case "lb", "lbs", "pound", "pounds":
		return value * 0.45359237, nil
	default:
		return 0, fmt.Errorf("unsupported mass unit %q", unit)
	}
}

// Millimeters normalizes explicitly identified length units. It never guesses a unit.
func Millimeters(value float64, unit string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "mm", "millimeter", "millimeters":
		return value, nil
	case "cm", "centimeter", "centimeters":
		return value * 10, nil
	case "m", "meter", "meters":
		return value * 1000, nil
	case "in", "inch", "inches":
		return value * 25.4, nil
	default:
		return 0, fmt.Errorf("unsupported length unit %q", unit)
	}
}

func nearlyEqual(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9
}

func canonicalInterface(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	replacer := strings.NewReplacer("_", " ", "-", " ", "/", " ")
	value = strings.Join(strings.Fields(replacer.Replace(value)), " ")
	switch value {
	case "IO LINK":
		return "IO-LINK"
	case "PROFI NET":
		return "PROFINET"
	case "ETHERNET IP":
		return "ETHERNET/IP"
	case "ETHERCAT":
		return "ETHERCAT"
	case "RS 485":
		return "RS-485"
	case "DIGITAL IO", "DIGITAL I O":
		return "DIGITAL I/O"
	case "MODBUS RTU":
		return "MODBUS RTU"
	case "MODBUS TCP":
		return "MODBUS TCP"
	default:
		return value
	}
}

var ipPattern = regexp.MustCompile(`(?i)^IP([0-6X])([0-9X])$`)

func parseIPRating(value string) (first, second int, ok bool) {
	matches := ipPattern.FindStringSubmatch(strings.TrimSpace(value))
	if matches == nil || strings.EqualFold(matches[1], "X") || strings.EqualFold(matches[2], "X") {
		return 0, 0, false
	}
	first, err1 := strconv.Atoi(matches[1])
	second, err2 := strconv.Atoi(matches[2])
	return first, second, err1 == nil && err2 == nil
}
