package migration

import "strings"

// observedScriptStepIDs maps the numeric instruction id FileMaker writes on
// every <Step> to the step's English name. Claris publishes no id table, so
// this one is built from observation only: each pair below was written by
// FileMaker Pro 26.0.2 itself, which names steps in English in Save a Copy as
// XML and in the client's language in the Database Design Report while keeping
// the same id. Every id observed was consistent across files and formats; an
// id absent from this table is never guessed and falls back to the name.
var observedScriptStepIDs = map[string]string{
	"1":   "Perform Script",
	"4":   "Go to Next Field",
	"6":   "Go to Layout",
	"7":   "New Record/Request",
	"10":  "Delete All Records",
	"16":  "Go to Record/Request/Page",
	"17":  "Go to Field",
	"22":  "Enter Find Mode",
	"23":  "Show All Records",
	"25":  "Omit Record",
	"28":  "Perform Find",
	"29":  "Show/Hide Toolbars",
	"31":  "Adjust Window",
	"34":  "Close File",
	"35":  "Import Records",
	"36":  "Export Records",
	"39":  "Sort Records",
	"42":  "Print Setup",
	"43":  "Print",
	"44":  "Exit Application",
	"47":  "Copy",
	"48":  "Paste",
	"55":  "Enter Browse Mode",
	"56":  "Insert Picture",
	"62":  "Pause/Resume Script",
	"63":  "Send Mail",
	"68":  "If",
	"69":  "Else",
	"70":  "End If",
	"71":  "Loop",
	"72":  "Exit Loop If",
	"73":  "End Loop",
	"74":  "Go to Related Record",
	"75":  "Commit Records/Requests",
	"76":  "Set Field",
	"77":  "Insert Calculated Result",
	"79":  "Freeze Window",
	"80":  "Refresh Window",
	"86":  "Set Error Capture",
	"87":  "Show Custom Dialog",
	"89":  "# (comment)",
	"90":  "Halt Script",
	"91":  "Replace Field Contents",
	"92":  "Show/Hide Text Ruler",
	"93":  "Beep",
	"99":  "Go to Portal Row",
	"103": "Exit Script",
	"104": "Delete Portal Row",
	"111": "Open URL",
	"121": "Close Window",
	"122": "New Window",
	"123": "Select Window",
	"125": "Else If",
	"131": "Insert File",
	"132": "Export Field Contents",
	"141": "Set Variable",
	"142": "Install Menu Set",
	"144": "Save Records as PDF",
	"145": "Go to Object",
	"147": "Set Field By Name",
	"160": "Insert from URL",
	"166": "Show/Hide Menubar",
}

// observedFrenchScriptSteps are the French step labels FileMaker Pro 26.0.2
// actually writes in an XML or HTML Database Design Report, paired with the
// English name the same instruction id carries in Save a Copy as XML. Unlike
// the hand-written labels in localizedScriptSteps, these were never inferred
// from documentation; they are used when a report carries no instruction id.
var observedFrenchScriptSteps = map[string]string{
	"exécuter script":                  "Perform Script",
	"rubrique suivante":                "Go to Next Field",
	"activer modèle":                   "Go to Layout",
	"nouvel enreg./requête":            "New Record/Request",
	"supprimer tous enreg.":            "Delete All Records",
	"afficher enreg/requête/page":      "Go to Record/Request/Page",
	"activer rubrique":                 "Go to Field",
	"mode recherche":                   "Enter Find Mode",
	"afficher tous les enreg.":         "Show All Records",
	"ignorer enregistrement":           "Omit Record",
	"exécuter la recherche":            "Perform Find",
	"afficher/masquer barres d'outils": "Show/Hide Toolbars",
	"ajuster fenêtre":                  "Adjust Window",
	"fermer fichier":                   "Close File",
	"importer enregistrements":         "Import Records",
	"exporter enregistrements":         "Export Records",
	"trier enregistrements":            "Sort Records",
	"configuration impression":         "Print Setup",
	"imprimer":                         "Print",
	"quitter application":              "Exit Application",
	"copier":                           "Copy",
	"coller":                           "Paste",
	"mode utilisation":                 "Enter Browse Mode",
	"insérer image":                    "Insert Picture",
	"suspendre/reprendre script":       "Pause/Resume Script",
	"envoyer courrier":                 "Send Mail",
	"si":                               "If",
	"sinon":                            "Else",
	"fin de si":                        "End If",
	"boucle":                           "Loop",
	"fin de boucle si":                 "Exit Loop If",
	"fin de boucle":                    "End Loop",
	"activer enregistrements liés":     "Go to Related Record",
	"valider enreg./requêtes":          "Commit Records/Requests",
	"définir rubrique":                 "Set Field",
	"insérer résultat du calcul":       "Insert Calculated Result",
	"figer fenêtre":                    "Freeze Window",
	"rafraîchir fenêtre":               "Refresh Window",
	"gestion erreurs":                  "Set Error Capture",
	"ouvrir boîte dial. person.":       "Show Custom Dialog",
	"# (commentaire)":                  "Comment",
	"interrompre script":               "Halt Script",
	"remplacer contenu rubrique":       "Replace Field Contents",
	"afficher/masquer réglette texte":  "Show/Hide Text Ruler",
	"signal sonore":                    "Beep",
	"activer rangée externe":           "Go to Portal Row",
	"fin de script":                    "Exit Script",
	"supprimer rangée externe":         "Delete Portal Row",
	"ouvrir url":                       "Open URL",
	"fermer fenêtre":                   "Close Window",
	"nouvelle fenêtre":                 "New Window",
	"sélectionner fenêtre":             "Select Window",
	"sinon si":                         "Else If",
	"insérer fichier":                  "Insert File",
	"exporter contenu de rubrique":     "Export Field Contents",
	"définir variable":                 "Set Variable",
	"installer jeu de menus":           "Install Menu Set",
	"sauvegarder en pdf":               "Save Records as PDF",
	"aller à l'objet":                  "Go to Object",
	"définir rubrique par nom":         "Set Field By Name",
	"insérer depuis url":               "Insert from URL",
	"afficher/masquer barre de menu":   "Show/Hide Menubar",
}

// stepResolution is how a source step was identified.
type stepResolution struct {
	Canonical string // name the converter and runner use; empty when File4Base has no such step
	English   string // FileMaker's English name, when the id or name identifies one
	Supported bool   // the client runner executes it
	Localized bool   // the written name differs from the English name
	Conflict  bool   // the id contradicted an English name; the name was used
}

// canonicalScriptStep resolves a step by its instruction id first, the only
// language-free key the document carries, and by its name otherwise.
func canonicalScriptStep(rawName, stepID string) stepResolution {
	byName, nameSupported, nameLocalized := canonicalScriptStepName(rawName)
	fromName := stepResolution{Canonical: byName, English: byName, Supported: nameSupported, Localized: nameLocalized}
	english, known := observedScriptStepIDs[strings.TrimSpace(stepID)]
	if !known {
		return fromName
	}
	if byName != "" && !nameLocalized && !strings.EqualFold(byName, english) {
		fromName.Conflict = true
		return fromName
	}
	res := stepResolution{English: english, Localized: !strings.EqualFold(strings.TrimSpace(rawName), english)}
	lower := strings.ToLower(english)
	if lower == "# (comment)" {
		lower = "comment"
	}
	if name, ok := supportedScriptSteps[lower]; ok {
		res.Canonical, res.Supported = name, true
	} else if name, ok := catalogOnlyScriptSteps[lower]; ok {
		res.Canonical = name
	}
	return res
}

// englishStepSuffix names the English step in a report about a localized one.
func englishStepSuffix(r stepResolution) string {
	if r.English == "" || !r.Localized {
		return ""
	}
	return ", " + r.English
}
