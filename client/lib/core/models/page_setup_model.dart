/// PageSetupModel defines print, paper size, orientation, margins, and printable area calculations.
class PageSetupModel {
  final String printer;
  final String paperSizeName; // 'A4', 'US Letter', 'US Legal', 'A3', 'A5', 'B5'
  final double paperWidthMm;
  final double paperHeightMm;
  final bool isLandscape;
  final double marginTopMm;
  final double marginBottomMm;
  final double marginLeftMm;
  final double marginRightMm;

  const PageSetupModel({
    this.printer = 'Any Printer',
    this.paperSizeName = 'A4',
    this.paperWidthMm = 210.0,
    this.paperHeightMm = 297.0,
    this.isLandscape = false,
    this.marginTopMm = 15.0,
    this.marginBottomMm = 15.0,
    this.marginLeftMm = 15.0,
    this.marginRightMm = 15.0,
  });

  // Effective dimensions in mm based on orientation
  double get effectiveWidthMm => isLandscape ? paperHeightMm : paperWidthMm;
  double get effectiveHeightMm => isLandscape ? paperWidthMm : paperHeightMm;

  // Printable dimensions in mm
  double get printableWidthMm =>
      (effectiveWidthMm - marginLeftMm - marginRightMm).clamp(10.0, 5000.0);
  double get printableHeightMm =>
      (effectiveHeightMm - marginTopMm - marginBottomMm).clamp(10.0, 5000.0);

  // Conversion: 72 points per inch, 25.4 mm per inch -> 1 mm = 2.83464567 pt
  static const double mmToPt = 2.83464567;

  // Dimensions in PostScript points (pt)
  double get totalWidthPt => effectiveWidthMm * mmToPt;
  double get totalHeightPt => effectiveHeightMm * mmToPt;
  double get printableWidthPt => printableWidthMm * mmToPt;
  double get printableHeightPt => printableHeightMm * mmToPt;
  double get marginLeftPt => marginLeftMm * mmToPt;
  double get marginRightPt => marginRightMm * mmToPt;
  double get marginTopPt => marginTopMm * mmToPt;
  double get marginBottomPt => marginBottomMm * mmToPt;

  PageSetupModel copyWith({
    String? printer,
    String? paperSizeName,
    double? paperWidthMm,
    double? paperHeightMm,
    bool? isLandscape,
    double? marginTopMm,
    double? marginBottomMm,
    double? marginLeftMm,
    double? marginRightMm,
  }) {
    return PageSetupModel(
      printer: printer ?? this.printer,
      paperSizeName: paperSizeName ?? this.paperSizeName,
      paperWidthMm: paperWidthMm ?? this.paperWidthMm,
      paperHeightMm: paperHeightMm ?? this.paperHeightMm,
      isLandscape: isLandscape ?? this.isLandscape,
      marginTopMm: marginTopMm ?? this.marginTopMm,
      marginBottomMm: marginBottomMm ?? this.marginBottomMm,
      marginLeftMm: marginLeftMm ?? this.marginLeftMm,
      marginRightMm: marginRightMm ?? this.marginRightMm,
    );
  }

  Map<String, dynamic> toJson() => {
    'printer': printer,
    'paper_size_name': paperSizeName,
    'paper_width_mm': paperWidthMm,
    'paper_height_mm': paperHeightMm,
    'is_landscape': isLandscape,
    'margin_top_mm': marginTopMm,
    'margin_bottom_mm': marginBottomMm,
    'margin_left_mm': marginLeftMm,
    'margin_right_mm': marginRightMm,
  };

  factory PageSetupModel.fromJson(Map<String, dynamic> json) {
    return PageSetupModel(
      printer: json['printer']?.toString() ?? 'Any Printer',
      paperSizeName: json['paper_size_name']?.toString() ?? 'A4',
      paperWidthMm: (json['paper_width_mm'] as num?)?.toDouble() ?? 210.0,
      paperHeightMm: (json['paper_height_mm'] as num?)?.toDouble() ?? 297.0,
      isLandscape: json['is_landscape'] as bool? ?? false,
      marginTopMm: (json['margin_top_mm'] as num?)?.toDouble() ?? 15.0,
      marginBottomMm: (json['margin_bottom_mm'] as num?)?.toDouble() ?? 15.0,
      marginLeftMm: (json['margin_left_mm'] as num?)?.toDouble() ?? 15.0,
      marginRightMm: (json['margin_right_mm'] as num?)?.toDouble() ?? 15.0,
    );
  }
}
