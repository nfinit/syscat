ALTER TABLE assets ADD COLUMN short_description TEXT NOT NULL DEFAULT '';
ALTER TABLE assets ADD COLUMN details TEXT NOT NULL DEFAULT '';
UPDATE assets SET
    short_description = rtrim(CASE WHEN instr(description,char(10))>0 THEN substr(description,1,instr(description,char(10))-1) ELSE description END, char(13)),
    details = CASE WHEN instr(description,char(10))>0 THEN substr(description,instr(description,char(10))+1) ELSE '' END;
