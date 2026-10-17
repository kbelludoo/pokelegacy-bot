package game

import (
	"testing"
)

func TestCatalogLoading(t *testing.T) {
	cat := GetCatalog()
	if len(cat.Spots) == 0 {
		t.Fatal("Esperava spots no catálogo, obteve 0")
	}
	if len(cat.Species) == 0 {
		t.Fatal("Esperava espécies no catálogo, obteve 0")
	}

	spot, ok := FindSpot("kanto_campos_de_pallet")
	if !ok {
		t.Fatal("Spot kanto_campos_de_pallet não encontrado")
	}
	if spot.Name != "Campos de Pallet" {
		t.Fatalf("Nome inesperado: %s", spot.Name)
	}

	sp, ok := GetSpecies(25) // Pikachu
	if !ok {
		t.Fatal("Pikachu (#25) não encontrado")
	}
	if sp.Name != "Pikachu" {
		t.Fatalf("Esperava Pikachu, obteve %s", sp.Name)
	}
}

func TestTypeEffectiveness(t *testing.T) {
	tests := []struct {
		atk      string
		def      string
		expected float64
	}{
		{"water", "fire", 2.0},
		{"fire", "grass", 2.0},
		{"grass", "water", 2.0},
		{"electric", "water", 2.0},
		{"electric", "ground", 0.0},
		{"ground", "electric", 2.0},
		{"normal", "normal", 1.0},
	}

	for _, tt := range tests {
		mult := TypeEffectiveness(tt.atk, tt.def)
		if mult != tt.expected {
			t.Errorf("TypeEffectiveness(%s, %s) = %v, esperava %v", tt.atk, tt.def, mult, tt.expected)
		}
	}
}

func TestBestRouteForTeam(t *testing.T) {
	// Treinador Lv. 10 com time de fogo
	route := BestRouteForTeam([]string{"fire"}, 10)
	if route.ID == "" {
		t.Fatal("Esperava rota válida, obteve vazia")
	}
	if route.Unlock > 10 {
		t.Fatalf("Rota %s requer nível %d, mas treinador é nível 10", route.ID, route.Unlock)
	}
}
