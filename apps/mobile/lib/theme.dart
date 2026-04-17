import 'package:flutter/material.dart';

class TalePanelColors {
  static const background = Color(0xFF0c0e12);
  static const surface    = Color(0xFF13151c);
  static const surface2   = Color(0xFF1a1d27);
  static const border     = Color(0xFF252833);
  static const primary    = Color(0xFF5b6ef5);
  static const success    = Color(0xFF22c55e);
  static const warning    = Color(0xFFf59e0b);
  static const danger     = Color(0xFFef4444);
  static const textPrimary = Color(0xFFf1f5f9);
  static const textMuted   = Color(0xFF64748b);
}

class TalePanelTheme {
  static ThemeData darkTheme() {
    return ThemeData(
      useMaterial3: true,
      brightness: Brightness.dark,
      fontFamily: 'Inter',
      scaffoldBackgroundColor: TalePanelColors.background,
      colorScheme: const ColorScheme.dark(
        primary: TalePanelColors.primary,
        surface: TalePanelColors.surface,
        error: TalePanelColors.danger,
        onPrimary: Colors.white,
        onSurface: TalePanelColors.textPrimary,
      ),
      appBarTheme: const AppBarTheme(
        backgroundColor: TalePanelColors.surface,
        foregroundColor: TalePanelColors.textPrimary,
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        titleTextStyle: TextStyle(
          fontFamily: 'Inter',
          fontSize: 18,
          fontWeight: FontWeight.w600,
          color: TalePanelColors.textPrimary,
        ),
      ),
      cardTheme: CardThemeData(
        color: TalePanelColors.surface,
        elevation: 0,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(12),
          side: const BorderSide(color: TalePanelColors.border),
        ),
        margin: EdgeInsets.zero,
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: TalePanelColors.surface2,
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(8),
          borderSide: const BorderSide(color: TalePanelColors.border),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(8),
          borderSide: const BorderSide(color: TalePanelColors.border),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(8),
          borderSide: const BorderSide(color: TalePanelColors.primary, width: 2),
        ),
        labelStyle: const TextStyle(color: TalePanelColors.textMuted),
        hintStyle: const TextStyle(color: TalePanelColors.textMuted),
      ),
      elevatedButtonTheme: ElevatedButtonThemeData(
        style: ElevatedButton.styleFrom(
          backgroundColor: TalePanelColors.primary,
          foregroundColor: Colors.white,
          minimumSize: const Size(double.infinity, 48),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(8),
          ),
          textStyle: const TextStyle(
            fontFamily: 'Inter',
            fontWeight: FontWeight.w600,
            fontSize: 16,
          ),
        ),
      ),
      dividerTheme: const DividerThemeData(
        color: TalePanelColors.border,
        thickness: 1,
        space: 1,
      ),
      bottomNavigationBarTheme: const BottomNavigationBarThemeData(
        backgroundColor: TalePanelColors.surface,
        selectedItemColor: TalePanelColors.primary,
        unselectedItemColor: TalePanelColors.textMuted,
        type: BottomNavigationBarType.fixed,
        elevation: 0,
      ),
    );
  }
}
