package migration

import "strings"

// This file holds what a Database Design Report calls things in a language
// other than English (#100, #103).
//
// A FileMaker DDR is localized: the HTML report of a French solution says
// "Nom de la table :" where an English one says "Table Name:", and an XML DDR
// writes a script step's name in the language the report was exported in
// ("Définir rubrique" for Set Field). The readers matched the English words,
// so a French report of a working solution converted to zero tables, zero
// fields and zero scripts while preflight reported success, and every
// recognizable step arrived as a disabled comment.
//
// Two rules keep this honest:
//
//   - Prefer structure over words. The anchors of an HTML report
//     (`BaseTableAnchor_`, `valScriptsSectionAnchor_`), the shape of its
//     tables, and the element names of an XML DDR are the same in every
//     language, so the readers key off those wherever they can and treat the
//     visible label as decoration.
//   - Translate only what is unambiguous. A name below is here because it can
//     mean one operation and no other. A step whose localized name is not
//     listed stays quarantined with its own name in the report, which is the
//     accurate outcome; a guess that mapped it onto the wrong operation would
//     not be.
//
// FileMaker also writes a numeric instruction id beside each step name
// (`<Step id="9" name="…">`). That id would be a language-free key, but
// Claris publishes no id table, so File4Base records the id as diagnostic
// metadata (`filemaker_step_id` on the imported step) rather than deciding an
// operation from a number it cannot verify.

// SupportedDDRLocales are the report languages the readers understand beyond
// their structure. It is stated in the conversion report's source format and
// in docs/specs/filemaker_migration.md.
var SupportedDDRLocales = []string{"English", "French"}

// localizedScriptSteps maps a lower-cased localized script step name onto the
// canonical English name the rest of the converter works with. The English
// names themselves live in supportedScriptSteps / catalogOnlyScriptSteps.
var localizedScriptSteps = map[string]string{
	// French — records
	"nouvel enreg./requête":                "New Record/Request",
	"nouvel enregistrement/requête":        "New Record/Request",
	"dupliquer enreg./requête":             "Duplicate Record/Request",
	"dupliquer l'enregistrement/requête":   "Duplicate Record/Request",
	"supprimer enreg./requête":             "Delete Record/Request",
	"supprimer l'enregistrement/requête":   "Delete Record/Request",
	"valider enreg./requêtes":              "Commit Records/Requests",
	"valider les enregistrements/requêtes": "Commit Records/Requests",
	"rétablir enreg./requête":              "Revert Record/Request",
	"omettre l'enregistrement":             "Omit Record",
	"trier les enregistrements":            "Sort Records",
	"afficher tous les enregistrements":    "Show All Records",
	"afficher les enregistrements omis":    "Show Omitted Only",

	// French — navigation and modes
	"atteindre la mise en page":                     "Go to Layout",
	"atteindre l'enregistrement/la requête/la page": "Go to Record/Request/Page",
	"mode utilisation":                              "Enter Browse Mode",
	"mode recherche":                                "Enter Find Mode",
	"mode prévisualisation":                         "Enter Preview Mode",
	"exécuter la recherche":                         "Perform Find",

	// French — fields, variables and control flow
	"définir rubrique":    "Set Field",
	"définir variable":    "Set Variable",
	"si":                  "If",
	"sinon":               "Else",
	"sinon si":            "Else If",
	"fin de si":           "End If",
	"boucle":              "Loop",
	"fin de boucle":       "End Loop",
	"sortie de boucle si": "Exit Loop If",

	// French — scripts, dialogs and the web
	"exécuter un script":                "Perform Script",
	"exécuter script":                   "Perform Script",
	"afficher boîte de dialogue perso.": "Show Custom Dialog",
	"ouvrir url":                        "Open URL",
	"commentaire":                       "Comment",
	"importer des enregistrements":      "Import Records",
	"exporter des enregistrements":      "Export Records",
	"insérer depuis url":                "Insert from URL",
	"imprimer":                          "Print",
}

// canonicalScriptStepName resolves a step name as written in the report — in
// English, or in one of the supported locales — to the canonical English name,
// saying whether the client runner executes it (supported) or the Script
// Workspace only holds it (catalogOnly).
func canonicalScriptStepName(rawName string) (canonical string, supported bool, localized bool) {
	lower := strings.ToLower(strings.TrimSpace(rawName))
	if name, ok := supportedScriptSteps[lower]; ok {
		return name, true, false
	}
	if name, ok := catalogOnlyScriptSteps[lower]; ok {
		return name, false, false
	}
	english, ok := localizedScriptSteps[lower]
	if !ok {
		return "", false, false
	}
	if name, ok := supportedScriptSteps[strings.ToLower(english)]; ok {
		return name, true, true
	}
	if name, ok := catalogOnlyScriptSteps[strings.ToLower(english)]; ok {
		return name, false, true
	}
	return "", false, false
}

// localizedFieldTypes maps the words a localized report uses for a field's
// type and kind onto the English words the field converters match on. Only
// the words that decide a column's type are listed.
var localizedFieldTypes = map[string]string{
	// French
	"texte":       "TEXT",
	"nombre":      "NUMBER",
	"numérique":   "NUMBER",
	"date":        "DATE",
	"heure":       "TIME",
	"horodatage":  "TIMESTAMP",
	"conteneur":   "CONTAINER",
	"calculé":     "CALCULATED",
	"calculée":    "CALCULATED",
	"calcul":      "CALCULATED",
	"statistique": "SUMMARY",
	"rubrique":    "", // "Rubrique" is "field": it says nothing about the type
	"normal":      "NORMAL",
	"normale":     "NORMAL",
	"global":      "GLOBAL",
	"globale":     "GLOBAL",
}

// englishFieldTypeWords rewrites a localized field type cell ("Calculé,
// Nombre") into the English words the type switches read ("CALCULATED,
// NUMBER"), leaving anything it does not know alone so that an unrecognized
// type is still visible to the caller.
func englishFieldTypeWords(rawType string) (string, []string) {
	if rawType == "" {
		return "", nil
	}
	fields := strings.FieldsFunc(rawType, func(r rune) bool {
		return r == ',' || r == ';' || r == '/' || r == '(' || r == ')'
	})
	var out []string
	var unknown []string
	for _, word := range fields {
		trimmed := strings.TrimSpace(word)
		if trimmed == "" {
			continue
		}
		upper := strings.ToUpper(trimmed)
		switch upper {
		case "TEXT", "NUMBER", "DATE", "TIME", "TIMESTAMP", "CONTAINER", "BINARY",
			"CALCULATED", "CALCULATION", "SUMMARY", "NORMAL", "GLOBAL", "BOOLEAN",
			"UNSTORED", "INDEXED", "UNINDEXED":
			out = append(out, upper)
			continue
		}
		if english, ok := localizedFieldTypes[strings.ToLower(trimmed)]; ok {
			if english != "" {
				out = append(out, english)
			}
			continue
		}
		unknown = append(unknown, trimmed)
		out = append(out, upper)
	}
	return strings.Join(out, ", "), unknown
}
