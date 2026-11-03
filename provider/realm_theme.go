package provider

import (
	"encoding/json"

	"github.com/pulumi/pulumi-go-provider/infer"
)

type RealmTheme struct {
	Appearance *string               `pulumi:"appearance,optional"`
	Values     map[string]string     `pulumi:"values,optional"`
	FontFaces  *[]RealmThemeFontFace `pulumi:"fontFaces,optional"`
}

type RealmThemeFontFace struct {
	Family string `pulumi:"family" json:"family"`
	File   string `pulumi:"file" json:"file"`
	Weight int    `pulumi:"weight" json:"weight"`
	Style  string `pulumi:"style" json:"style"`
}

type realmThemeWire struct {
	Appearance *string               `json:"appearance,omitempty"`
	Values     map[string]*string    `json:"values,omitempty"`
	FontFaces  *[]RealmThemeFontFace `json:"font_faces,omitempty"`
}

func (t *RealmTheme) Annotate(a infer.Annotator) {
	a.Describe(&t.Appearance, "`system` lets each visitor choose; `light` and `dark` fix the mode for the whole realm.")
	a.Describe(&t.Values, "Theme settings by name: Lattice tokens such as `--lt-primary` and settings such as `logo-url`. A file setting holds an `https` URL or `asset:<name>` for a RealmThemeAsset.")
	a.Describe(&t.FontFaces, "The font files the font stacks name.")
}

func (f *RealmThemeFontFace) Annotate(a infer.Annotator) {
	a.Describe(&f.Family, "The font family this file belongs to, as the font stack settings name it.")
	a.Describe(&f.File, "An `https` URL, or `asset:<name>` for a RealmThemeAsset.")
	a.Describe(&f.Weight, "The weight the file holds, from 1 to 1000.")
	a.Describe(&f.Style, "The style the file holds: `normal` or `italic`.")
}

// Unlike realm settings, a theme can be reset: what the previous inputs managed and the new ones
// leave out goes back to the instance's look.
func themeToWire(theme, previous *RealmTheme) *realmThemeWire {
	if theme == nil && previous == nil {
		return nil
	}
	if theme == nil {
		theme = &RealmTheme{}
	}
	wire := &realmThemeWire{Appearance: theme.Appearance, FontFaces: theme.FontFaces}
	if wire.FontFaces != nil {
		faces := append([]RealmThemeFontFace{}, *wire.FontFaces...)
		wire.FontFaces = &faces
	}
	values := map[string]*string{}
	for name, value := range theme.Values {
		values[name] = &value
	}
	if previous != nil {
		if wire.Appearance == nil && previous.Appearance != nil {
			system := "system"
			wire.Appearance = &system
		}
		if wire.FontFaces == nil && previous.FontFaces != nil {
			wire.FontFaces = &[]RealmThemeFontFace{}
		}
		for name := range previous.Values {
			if _, kept := theme.Values[name]; !kept {
				values[name] = nil
			}
		}
	}
	if len(values) > 0 {
		wire.Values = values
	}
	return wire
}

// Only what the inputs manage reaches the state, so a value set in the console is no drift.
func themeFromResponse(body []byte, managed *RealmTheme) (*RealmTheme, error) {
	if managed == nil {
		return nil, nil
	}
	var response struct {
		Data struct {
			Theme struct {
				Appearance string               `json:"appearance"`
				Values     map[string]string    `json:"values"`
				FontFaces  []RealmThemeFontFace `json:"font_faces"`
			} `json:"theme"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	theme := response.Data.Theme
	result := &RealmTheme{}
	if managed.Appearance != nil {
		result.Appearance = &theme.Appearance
	}
	if managed.FontFaces != nil {
		faces := append([]RealmThemeFontFace{}, theme.FontFaces...)
		result.FontFaces = &faces
	}
	if managed.Values != nil {
		result.Values = map[string]string{}
		for name := range managed.Values {
			if value, exists := theme.Values[name]; exists {
				result.Values[name] = value
			}
		}
	}
	return result, nil
}
