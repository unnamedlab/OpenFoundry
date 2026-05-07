package domain

import (
	"fmt"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

// SynthesizeEntityRecords ports `deduplication::synthesize_entity_records`.
func SynthesizeEntityRecords(entityType string, config models.ResolutionJobConfig) []models.EntityRecord {
	if strings.EqualFold(entityType, "organization") {
		return synthesizeOrganizationRecords(config)
	}
	return synthesizePersonRecords(config)
}

type profile struct {
	names    [3]string
	emails   [3]string
	phones   [3]string
	city     string
	company  string
	address  string
}

var personProfiles = []profile{
	{
		names:   [3]string{"John Smith", "Jon Smyth", "J. Smith"},
		emails:  [3]string{"john.smith@acme.com", "jon.smyth@acme.com", "john.smith+support@acme.com"},
		phones:  [3]string{"+1 415 555 0100", "+1 (415) 555-0100", "4155550100"},
		city:    "San Francisco",
		company: "Acme Logistics",
		address: "100 Market St",
	},
	{
		names:   [3]string{"Ana Perez", "Anna Peres", "A. Perez"},
		emails:  [3]string{"ana.perez@nova.io", "anna.peres@nova.io", "ana.perez+ops@nova.io"},
		phones:  [3]string{"+34 91 555 0101", "+34 915550101", "91-555-0101"},
		city:    "Madrid",
		company: "Nova Energy",
		address: "Calle Gran Via 44",
	},
	{
		names:   [3]string{"Mark Johnson", "Marc Jonson", "M. Johnson"},
		emails:  [3]string{"mark.johnson@northwind.co", "marc.jonson@northwind.co", "mark.johnson+help@northwind.co"},
		phones:  [3]string{"+1 646 555 0102", "+1-646-555-0102", "6465550102"},
		city:    "New York",
		company: "Northwind Trading",
		address: "215 Madison Ave",
	},
	{
		names:   [3]string{"Mei Chen", "May Chen", "M. Chen"},
		emails:  [3]string{"mei.chen@harbor.ai", "may.chen@harbor.ai", "mei.chen+crm@harbor.ai"},
		phones:  [3]string{"+65 6555 0103", "+65-6555-0103", "65550103"},
		city:    "Singapore",
		company: "Harbor AI",
		address: "12 Anson Rd",
	},
}

var organizationProfiles = []profile{
	{
		names:   [3]string{"Acme Logistics", "ACME Logstics", "Acme Log."},
		emails:  [3]string{"ops@acme-logistics.com", "support@acme-logistics.com", "hello@acme-logistics.com"},
		phones:  [3]string{"+1 415 555 1000", "+1 (415) 555-1000", "4155551000"},
		city:    "San Francisco",
		company: "Acme Logistics",
		address: "100 Market St",
	},
	{
		names:   [3]string{"Nova Energy", "Nova Energi", "Nova Eng."},
		emails:  [3]string{"info@novaenergy.io", "ops@novaenergy.io", "hello@novaenergy.io"},
		phones:  [3]string{"+34 91 555 1001", "+34 915551001", "915551001"},
		city:    "Madrid",
		company: "Nova Energy",
		address: "Calle Gran Via 44",
	},
	{
		names:   [3]string{"Northwind Trading", "North Wind Trading", "Northwind Trdg"},
		emails:  [3]string{"ops@northwind.co", "support@northwind.co", "hello@northwind.co"},
		phones:  [3]string{"+1 646 555 1002", "+1-646-555-1002", "6465551002"},
		city:    "New York",
		company: "Northwind Trading",
		address: "215 Madison Ave",
	},
}

func synthesizePersonRecords(config models.ResolutionJobConfig) []models.EntityRecord {
	return buildRecords(config, personProfiles, "person")
}

func synthesizeOrganizationRecords(config models.ResolutionJobConfig) []models.EntityRecord {
	return buildRecords(config, organizationProfiles, "organization")
}

func buildRecords(config models.ResolutionJobConfig, profiles []profile, recordType string) []models.EntityRecord {
	sourceLabels := config.SourceLabels
	if len(sourceLabels) == 0 {
		sourceLabels = []string{"crm", "erp", "support"}
	}

	targetCount := int(config.RecordCount)
	if targetCount < 9 {
		targetCount = 9
	}

	records := make([]models.EntityRecord, 0, targetCount)
	for cycle := 0; cycle < 4; cycle++ {
		for profileIndex, p := range profiles {
			for sourceIndex, source := range sourceLabels {
				name := p.names[sourceIndex%len(p.names)]
				email := p.emails[sourceIndex%len(p.emails)]
				phone := p.phones[sourceIndex%len(p.phones)]
				externalID := fmt.Sprintf("%s-%d-%d", source, profileIndex+1, cycle+1)

				confidence := 0.82 - float32(cycle)*0.03
				switch {
				case confidence < 0.45:
					confidence = 0.45
				case confidence > 0.95:
					confidence = 0.95
				}

				records = append(records, models.EntityRecord{
					RecordID:    fmt.Sprintf("%s:%s:%s", source, recordType, externalID),
					Source:      source,
					ExternalID:  externalID,
					DisplayName: name,
					Confidence:  confidence,
					Attributes: map[string]any{
						"name":        name,
						"email":       email,
						"phone":       phone,
						"city":        p.city,
						"company":     p.company,
						"address":     p.address,
						"source_rank": sourceIndex + 1,
					},
				})

				if len(records) >= targetCount {
					return records
				}
			}
		}
	}
	return records
}
